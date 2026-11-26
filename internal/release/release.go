package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pug-sh/leo/internal/config"
	"github.com/pug-sh/leo/internal/gh"
	"github.com/pug-sh/leo/internal/git"
)

// wait is how old the oldest unreleased commit gets before Leo opens a reminder.
const wait = 24 * time.Hour

const sdkTopic = "pug-sdk"

type Releaser struct {
	c   *gh.Client
	cfg *config.Config
	// cloneURL gives the URL to clone a repo from.
	cloneURL func(repo string) string
	now      time.Time
}

func New(c *gh.Client, cfg *config.Config, now time.Time) *Releaser {
	return &Releaser{c: c, cfg: cfg, now: now, cloneURL: func(repo string) string {
		return "https://github.com/" + gh.Org + "/" + repo + ".git"
	}}
}

// Run keeps one reminder issue per repo listing the changes on its default branch that ship to
// users but aren't in a release yet.
func (r *Releaser) Run(ctx context.Context) error {
	repos, err := r.c.Repos(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, repo := range repos {
		if err := r.runRepo(ctx, repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo.Name, err))
		}
	}
	return errors.Join(errs...)
}

// State is what a repo's default branch holds since its latest release.
type State struct {
	Repo   string
	Branch string
	// Tag is the highest release tag reachable from Branch, or empty if none is.
	Tag string
	// Elsewhere is the highest release tag, when none is reachable from Branch.
	Elsewhere string
	// Unreleased are the first-parent commits after Tag that touch shipped files, oldest first.
	Unreleased []Commit
	// Tip is the time of the branch's last commit.
	Tip time.Time
}

func (r *Releaser) runRepo(ctx context.Context, repo gh.Repo) error {
	url := r.cloneURL(repo.Name)
	tags, err := remoteTags(ctx, url)
	if err != nil {
		return err
	}
	if _, ok := Latest(tags); !ok {
		return nil
	}
	dir, err := os.MkdirTemp("", "leo-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if _, err := git.Run(ctx, "", "clone", "--quiet", "--bare", "--filter=blob:none", url, dir); err != nil {
		return err
	}
	st, err := r.state(ctx, dir, repo)
	if err != nil {
		return err
	}
	return r.remind(ctx, st)
}

func remoteTags(ctx context.Context, url string) ([]string, error) {
	lines, err := git.Lines(ctx, "", "ls-remote", "--tags", "--refs", url)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, field := range lines {
		if tag, ok := strings.CutPrefix(field, "refs/tags/"); ok {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func (r *Releaser) state(ctx context.Context, dir string, repo gh.Repo) (State, error) {
	st := State{Repo: repo.Name, Branch: repo.DefaultBranch}
	branch := "refs/heads/" + repo.DefaultBranch
	tip, err := git.Run(ctx, dir, "log", "-1", "--format=%ct", branch)
	if err != nil {
		return st, err
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(tip)), 10, 64)
	if err != nil {
		return st, fmt.Errorf("time of %s: %w", branch, err)
	}
	st.Tip = time.Unix(sec, 0).UTC()
	merged, err := git.Lines(ctx, dir, "tag", "--merged", branch)
	if err != nil {
		return st, err
	}
	tag, ok := Latest(merged)
	if !ok {
		all, err := git.Lines(ctx, dir, "tag")
		if err != nil {
			return st, err
		}
		st.Elsewhere, _ = Latest(all)
		return st, nil
	}
	st.Tag = tag
	commits, err := FirstParent(ctx, dir, "refs/tags/"+tag, branch)
	if err != nil {
		return st, err
	}
	rc := r.cfg.Repo(repo.Name)
	sdk := repo.HasTopic(sdkTopic)
	for _, c := range commits {
		for _, f := range c.Files {
			if Shipped(f, sdk, rc.Unshipped) {
				st.Unreleased = append(st.Unreleased, c)
				break
			}
		}
	}
	return st, nil
}

// remind opens, updates or closes the repo's reminder to match st.
func (r *Releaser) remind(ctx context.Context, st State) error {
	issues, err := r.c.LeoIssues(ctx, st.Repo, gh.MarkerRelease)
	if err != nil {
		return err
	}
	var open, last *gh.Issue
	if len(issues) > 0 {
		last = &issues[0]
	}
	for i := range issues {
		if issues[i].Open {
			open = &issues[i]
			break
		}
	}

	r.logState(st)
	if st.Tag != "" && len(st.Unreleased) == 0 {
		if open == nil {
			return nil
		}
		return r.c.CloseIssue(ctx, st.Repo, open.Number, fmt.Sprintf("%s covers %s, so I'm closing this.", st.Tag, st.Branch))
	}
	title, body := Title(st), Body(st)
	if open != nil {
		if open.Title == title && open.Body == body {
			return nil
		}
		return r.c.EditIssue(ctx, st.Repo, open.Number, title, body)
	}

	newest := st.Tip
	if st.Tag != "" {
		if r.now.Sub(st.Unreleased[0].Time) < wait {
			return nil
		}
		newest = st.Unreleased[len(st.Unreleased)-1].Time
	}
	if last != nil {
		// Someone closed Leo's last reminder: only a commit since brings it back.
		byPerson, err := r.c.ClosedByPerson(ctx, st.Repo, last.Number)
		if err != nil {
			return err
		}
		if byPerson && !newest.After(last.ClosedAt) {
			return nil
		}
	}
	_, err = r.c.CreateIssue(ctx, st.Repo, title, body)
	return err
}

func (r *Releaser) logState(st State) {
	switch {
	case st.Tag == "":
		r.c.Logf("%s: no release tagged on %s", st.Repo, st.Branch)
	case len(st.Unreleased) == 0:
		r.c.Logf("%s: %s covers %s", st.Repo, st.Tag, st.Branch)
	default:
		oldest := st.Unreleased[0].Time
		r.c.Logf("%s: %d unreleased since %s, the oldest from %s (%s ago)", st.Repo, len(st.Unreleased), st.Tag,
			oldest.Format(time.DateTime), r.now.Sub(oldest).Round(time.Minute))
	}
}

func Title(st State) string {
	if st.Tag == "" {
		return "Unreleased changes: no release tagged on " + st.Branch
	}
	return "Unreleased changes since " + st.Tag
}

// maxListed keeps a reminder well inside GitHub's limit on an issue's length.
const maxListed = 250

func Body(st State) string {
	repoURL := "https://github.com/" + gh.Org + "/" + st.Repo
	var b strings.Builder
	if st.Tag == "" {
		fmt.Fprintf(&b, "No release is tagged on %s", st.Branch)
		if st.Elsewhere != "" {
			fmt.Fprintf(&b, ": the latest, [%s](%s/releases/tag/%s), isn't reachable from it", st.Elsewhere, repoURL, st.Elsewhere)
		}
		fmt.Fprintf(&b, ". Tag the next release on %s, so I can tell what's unreleased.\n\n", st.Branch)
		b.WriteString(gh.Marker(gh.MarkerRelease))
		return b.String()
	}

	changes := "changes"
	if len(st.Unreleased) == 1 {
		changes = "change"
	}
	fmt.Fprintf(&b, "%s has %d %s since [%s](%s/releases/tag/%s) that ship to users but aren't released yet ([compare](%s/compare/%s...%s)).\n",
		st.Branch, len(st.Unreleased), changes, st.Tag, repoURL, st.Tag, repoURL, st.Tag, st.Branch)
	var prs, direct []string
	for _, c := range st.Unreleased {
		if c.PR != 0 {
			prs = append(prs, fmt.Sprintf("- #%d %s", c.PR, c.Title))
		} else {
			direct = append(direct, fmt.Sprintf("- %s %s", c.SHA[:7], c.Title))
		}
	}
	remaining := maxListed
	section := func(heading string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n**%s**\n\n", heading)
		shown := min(len(lines), remaining)
		for _, line := range lines[:shown] {
			b.WriteString(line + "\n")
		}
		if shown < len(lines) {
			fmt.Fprintf(&b, "- and %d more\n", len(lines)-shown)
		}
		remaining -= shown
	}
	section("Merged pull requests", prs)
	section("Direct commits", direct)
	fmt.Fprintf(&b, "\nI'll keep this current as %s moves, and close it once a release covers %s.\n\n", st.Branch, st.Branch)
	b.WriteString(gh.Marker(gh.MarkerRelease))
	return b.String()
}
