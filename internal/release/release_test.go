package release

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pug-sh/leo/internal/config"
	"github.com/pug-sh/leo/internal/gh"
	"github.com/pug-sh/leo/internal/gh/ghtest"
	"github.com/pug-sh/leo/internal/git/gittest"
)

var now = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// newFixture builds a repo with releases v0.1.0 and v0.1.1 on main, v0.2.0 on a branch that main
// never merged, and then, on main, a workflow change, a lockfile bump, a merged pull request, a
// squashed one and a direct commit.
func newFixture(t *testing.T, root, name string) *gittest.Repo {
	f := gittest.Init(t, filepath.Join(root, name))
	f.Commit("2026-11-01T00:00:00Z", "Start", "README.md", "lib.go")
	f.Git("2026-11-01T00:00:00Z", "tag", "v0.1.0")
	f.Commit("2026-11-02T00:00:00Z", "Release v0.1.1", "lib.go")
	f.Git("2026-11-02T00:00:00Z", "tag", "v0.1.1")
	f.Git("2026-11-03T00:00:00Z", "checkout", "--quiet", "-b", "backport")
	f.Commit("2026-11-03T00:00:00Z", "Backport", "lib.go")
	f.Git("2026-11-03T00:00:00Z", "tag", "v0.2.0")
	f.Git("2026-11-03T00:00:00Z", "checkout", "--quiet", "main")

	f.Commit("2026-11-20T10:00:00Z", "Pin actions", ".github/workflows/ci.yml")
	f.Commit("2026-11-20T11:00:00Z", "Bump a test dependency", "go.sum")
	f.Git("2026-11-20T12:00:00Z", "checkout", "--quiet", "-b", "feat")
	f.Commit("2026-11-20T12:00:00Z", "Write the feature", "feat.go", ".github/dependabot.yml")
	f.Git("2026-11-21T00:00:00Z", "checkout", "--quiet", "main")
	f.Git("2026-11-21T00:00:00Z", "merge", "--quiet", "--no-ff", "feat", "-m", "Merge pull request #7 from pug-sh/feat", "-m", "Add the feature")
	f.Commit("2026-11-22T00:00:00Z", "Fix a typo (#9)", "docs/guide.md")
	f.Commit("2026-11-23T00:00:00Z", "Tidy", "lib.go")
	return f
}

func TestState(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t, root, "sdk-x")
	tidy := f.Git("2026-11-23T00:00:00Z", "rev-parse", "HEAD")

	r := &Releaser{cfg: &config.Config{}}
	st, err := r.state(context.Background(), f.Dir, gh.Repo{Name: "sdk-x", DefaultBranch: "main", Topics: []string{"pug-sdk"}})
	if err != nil {
		t.Fatal(err)
	}
	if st.Tag != "v0.1.1" {
		t.Errorf("Tag = %q, want v0.1.1, the highest reachable from main", st.Tag)
	}
	var got []string
	for _, c := range st.Unreleased {
		got = append(got, fmt.Sprintf("%d %s %s %v", c.PR, c.Title, c.Time.Format(time.DateOnly), c.Files))
	}
	want := []string{
		"7 Add the feature 2026-11-21 [.github/dependabot.yml feat.go]",
		"9 Fix a typo 2026-11-22 [docs/guide.md]",
		"0 Tidy 2026-11-23 [lib.go]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Unreleased:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if st.Unreleased[2].SHA != tidy {
		t.Errorf("SHA = %s, want %s", st.Unreleased[2].SHA, tidy)
	}

	r.cfg = &config.Config{Repos: map[string]config.Repo{"sdk-x": {Unshipped: []string{"docs/", "lib.go"}}}}
	st, err = r.state(context.Background(), f.Dir, gh.Repo{Name: "sdk-x", DefaultBranch: "main", Topics: []string{"pug-sdk"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Unreleased) != 1 || st.Unreleased[0].PR != 7 {
		t.Errorf("with overrides, Unreleased = %+v, want only #7", st.Unreleased)
	}
}

func TestStateWithoutTagOnMain(t *testing.T) {
	root := t.TempDir()
	f := gittest.Init(t, filepath.Join(root, "x"))
	f.Commit("2026-11-01T00:00:00Z", "Start", "a.go")
	f.Git("2026-11-01T00:00:00Z", "checkout", "--quiet", "-b", "release")
	f.Commit("2026-11-02T00:00:00Z", "Release", "a.go")
	f.Git("2026-11-02T00:00:00Z", "tag", "0.1.0")
	f.Git("2026-11-02T00:00:00Z", "checkout", "--quiet", "main")

	r := &Releaser{cfg: &config.Config{}}
	st, err := r.state(context.Background(), f.Dir, gh.Repo{Name: "x", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Tag != "" || st.Elsewhere != "0.1.0" {
		t.Errorf("Tag = %q, Elsewhere = %q", st.Tag, st.Elsewhere)
	}
	if got := Title(st); got != "Unreleased changes: no release tagged on main" {
		t.Errorf("Title = %q", got)
	}
	if body := Body(st); !strings.Contains(body, "the latest, [0.1.0](https://github.com/pug-sh/x/releases/tag/0.1.0), isn't reachable from it") {
		t.Errorf("Body:\n%s", body)
	}
}

func TestRunDryRun(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t, root, "sdk-x")
	head := f.Git("2026-11-23T00:00:00Z", "rev-parse", "--short=7", "HEAD")
	untagged := gittest.Init(t, filepath.Join(root, "app"))
	untagged.Commit("2026-11-01T00:00:00Z", "Start", "a.go")

	s := ghtest.New(t)
	s.REST("GET", "/orgs/pug-sh/repos", 200, `[
	  {"name": "app", "visibility": "public", "default_branch": "main"},
	  {"name": "sdk-x", "visibility": "public", "default_branch": "main", "topics": ["pug-sdk"]}
	]`)
	s.REST("GET", "/repos/pug-sh/sdk-x/issues", 200, `[]`)

	c, out := s.Client(true)
	r := New(c, &config.Config{}, now)
	r.cloneURL = func(repo string) string { return filepath.Join(root, repo) }
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"sdk-x: 3 unreleased since v0.1.1, the oldest from 2026-11-21 00:00:00",
		`dry-run: sdk-x: open issue "Unreleased changes since v0.1.1"`,
		"    | main has 3 changes since [v0.1.1](https://github.com/pug-sh/sdk-x/releases/tag/v0.1.1) that ship to users",
		"([compare](https://github.com/pug-sh/sdk-x/compare/v0.1.1...main))",
		"    | - #7 Add the feature\n    | - #9 Fix a typo\n",
		"    | **Direct commits**\n    | \n    | - " + head + " Tidy\n",
		"    | <!-- leo:release -->",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "app") {
		t.Errorf("a repo without release tags got a reminder:\n%s", out)
	}
}

func TestRemind(t *testing.T) {
	st := State{Repo: "sdk-x", Branch: "main", Tag: "v0.1.1", Unreleased: []Commit{
		{SHA: "aaaaaaaaaa", Time: now.Add(-30 * time.Hour), PR: 7, Title: "Add the feature"},
		{SHA: "bbbbbbbbbb", Time: now.Add(-2 * time.Hour), Title: "Tidy"},
	}}
	released := State{Repo: "sdk-x", Branch: "main", Tag: "v0.1.2"}
	fresh := State{Repo: "sdk-x", Branch: "main", Tag: "v0.1.1", Unreleased: []Commit{{SHA: "cccccccccc", Time: now.Add(-time.Hour), Title: "New"}}}
	issue := func(number int, state, title, body, closedAt string) string {
		closed := "null"
		if closedAt != "" {
			closed = fmt.Sprintf("%q", closedAt)
		}
		return fmt.Sprintf(`{"number": %d, "state": %q, "title": %q, "body": %q, "closed_at": %s, "user": {"login": "leo-the-pug[bot]"}}`,
			number, state, title, body, closed)
	}

	tests := []struct {
		name     string
		st       State
		issues   string
		closedBy string
		want     []string
	}{
		{"opens a reminder", st, `[]`, "", []string{`open issue "Unreleased changes since v0.1.1"`}},
		{"waits a day", fresh, `[]`, "", nil},
		{"leaves a current reminder", st, "[" + issue(4, "open", Title(st), Body(st), "") + "]", "", nil},
		{"updates a stale reminder", st, "[" + issue(4, "open", "Unreleased changes since v0.1.0", "Old.\n\n"+gh.Marker(gh.MarkerRelease), "") + "]", "",
			[]string{`sdk-x#4: update to "Unreleased changes since v0.1.1"`}},
		{"closes once released", released, "[" + issue(4, "open", Title(st), Body(st), "") + "]", "",
			[]string{`sdk-x#4: comment "v0.1.2 covers main, so I'm closing this."`, "sdk-x#4: close"}},
		{"respects a person's close", st, "[" + issue(4, "closed", Title(st), Body(st), "2026-11-26T11:00:00Z") + "]", "poluruprvn", nil},
		{"reopens after a new commit", st, "[" + issue(4, "closed", Title(st), Body(st), "2026-11-26T09:00:00Z") + "]", "poluruprvn",
			[]string{`open issue "Unreleased changes since v0.1.1"`}},
		{"reopens after its own close", st, "[" + issue(4, "closed", Title(st), Body(st), "2026-11-26T11:00:00Z") + "]", gh.BotLogin,
			[]string{`open issue "Unreleased changes since v0.1.1"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ghtest.New(t)
			s.REST("GET", "/repos/pug-sh/sdk-x/issues", 200, tt.issues)
			s.REST("GET", "/repos/pug-sh/sdk-x/issues/4", 200, fmt.Sprintf(`{"number": 4, "state": "closed", "closed_by": {"login": %q}}`, tt.closedBy))
			c, out := s.Client(true)
			r := New(c, &config.Config{}, now)
			if err := r.remind(context.Background(), tt.st); err != nil {
				t.Fatal(err)
			}
			var got []string
			for line := range strings.SplitSeq(out.String(), "\n") {
				if action, ok := strings.CutPrefix(line, "dry-run: "); ok {
					got = append(got, strings.TrimPrefix(action, "sdk-x: "))
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("actions = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBodyListsAtMostMaxListed(t *testing.T) {
	st := State{Repo: "pug", Branch: "main", Tag: "v1.0.0"}
	for i := range maxListed + 5 {
		st.Unreleased = append(st.Unreleased, Commit{SHA: fmt.Sprintf("%040d", i), PR: i + 1, Title: "Change"})
	}
	st.Unreleased = append(st.Unreleased, Commit{SHA: strings.Repeat("f", 40), Title: "Direct"})
	body := Body(st)
	if n := strings.Count(body, "\n- "); n != maxListed+2 {
		t.Errorf("%d list lines, want %d", n, maxListed+2)
	}
	if !strings.Contains(body, "- and 5 more\n") || !strings.Contains(body, "**Direct commits**\n\n- and 1 more\n") {
		t.Errorf("body doesn't say what it left out:\n%s", body[len(body)-300:])
	}
}
