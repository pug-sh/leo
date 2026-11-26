package protos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pug-sh/leo/internal/gh"
	"github.com/pug-sh/leo/internal/git"
)

// SyncInput is what sync.yml's push job hands Leo for one SDK.
type SyncInput struct {
	Repo string
	// Branch is the SDK's default branch, and Base the commit of it that the codegen job ran on.
	Branch string
	Base   string
	// Dir is a checkout of Base, which Patch, the codegen job's diff, applies to.
	Dir     string
	Patch   string
	Log     string
	BSR     BSRCommit
	Codegen string
	RunURL  string
}

const logLines, logBytes = 150, 50000

// Sync turns the codegen job's diff into a commit on leo/sync-protos and a pull request, or
// reports why it can't.
func Sync(ctx context.Context, c *gh.Client, in SyncInput) error {
	switch in.Codegen {
	case "success":
	case "cancelled", "skipped":
		c.Logf("%s: the codegen job was %s", in.Repo, in.Codegen)
		return nil
	default:
		log, err := os.ReadFile(in.Log)
		if err != nil {
			log = []byte(fmt.Sprintf("The codegen job left no log: %v", err))
		}
		return ReportFailure(ctx, c, Failure{
			Repo: in.Repo, BSR: in.BSR, What: "the codegen job failed", Detail: Tail(string(log), logLines, logBytes), RunURL: in.RunURL,
		})
	}
	if err := push(ctx, c, in); err != nil {
		failure := Failure{Repo: in.Repo, BSR: in.BSR, What: "I couldn't push the sync", Detail: err.Error(), RunURL: in.RunURL}
		return errors.Join(err, ReportFailure(ctx, c, failure))
	}
	return nil
}

func push(ctx context.Context, c *gh.Client, in SyncInput) error {
	changes, err := applyPatch(ctx, in.Dir, in.Patch)
	if err != nil {
		return err
	}
	synced := fmt.Sprintf("Synced to `%s`, so I'm closing this.", in.BSR.Short())
	if len(changes) == 0 {
		c.Logf("%s: the codegen job changed nothing", in.Repo)
		return CloseFailures(ctx, c, in.Repo, synced)
	}

	pr, err := c.OpenPR(ctx, in.Repo, gh.SyncBranch)
	if err != nil {
		return err
	}
	tip, err := c.Branch(ctx, in.Repo, gh.SyncBranch)
	if err != nil {
		return err
	}
	if tip != "" {
		others, err := c.CommitsByOthers(ctx, in.Repo, in.Branch, gh.SyncBranch)
		if err != nil {
			return err
		}
		if others {
			return leaveAlone(ctx, c, in, pr)
		}
		if pr == nil {
			closed, merged, err := c.LastClosedPR(ctx, in.Repo, gh.SyncBranch)
			if err != nil {
				return err
			}
			if args, _ := gh.ParseMarker(closedBody(closed), gh.MarkerProtos); closed != nil && !merged && len(args) > 0 && args[0] == in.BSR.Commit {
				c.Logf("%s: someone closed #%d, the sync to %s, so I've left it closed", in.Repo, closed.Number, in.BSR.Short())
				return nil
			}
		}
	}

	baseTree, err := c.CommitTree(ctx, in.Repo, in.Base)
	if err != nil {
		return err
	}
	tree, err := c.CreateTree(ctx, in.Repo, baseTree, changes)
	if err != nil {
		return err
	}
	if tip != "" && tree != "" {
		tipTree, err := c.CommitTree(ctx, in.Repo, tip)
		if err != nil {
			return err
		}
		if tipTree == tree {
			c.Logf("%s: %s already holds this sync", in.Repo, gh.SyncBranch)
			if err := ensurePR(ctx, c, in, pr, changes, 0); err != nil {
				return err
			}
			return CloseFailures(ctx, c, in.Repo, synced)
		}
	}
	sha, err := c.CreateCommit(ctx, in.Repo, commitMessage(in.BSR), tree, in.Base)
	if err != nil {
		return err
	}

	replaced := 0
	if tip == "" {
		err = c.CreateBranch(ctx, in.Repo, gh.SyncBranch, sha)
	} else {
		err = c.ForceBranch(ctx, in.Repo, gh.SyncBranch, sha)
		if errors.Is(err, gh.ErrWorkflowRefused) {
			// Deleting the branch closes its pull request; a fresh branch carries no workflow changes.
			c.Logf("%s: %s changed .github/workflows/ since %s's base, so I'm replacing the branch", in.Repo, in.Branch, gh.SyncBranch)
			if err = c.DeleteBranch(ctx, in.Repo, gh.SyncBranch); err == nil {
				if pr != nil {
					replaced, pr = pr.Number, nil
				}
				err = c.CreateBranch(ctx, in.Repo, gh.SyncBranch, sha)
			}
		}
	}
	if err != nil {
		return err
	}
	if err := ensurePR(ctx, c, in, pr, changes, replaced); err != nil {
		return err
	}
	return CloseFailures(ctx, c, in.Repo, synced)
}

func closedBody(pr *gh.PR) string {
	if pr == nil {
		return ""
	}
	return pr.Body
}

func leaveAlone(ctx context.Context, c *gh.Client, in SyncInput, pr *gh.PR) error {
	if pr == nil {
		c.Warnf("%s: %s has commits from someone else and no open pull request: delete the branch to resume syncing", in.Repo, gh.SyncBranch)
		return nil
	}
	done, err := c.LeoCommented(ctx, in.Repo, pr.Number, gh.MarkerProtosLocked, in.BSR.Commit)
	if err != nil || done {
		return err
	}
	body := fmt.Sprintf("pug's protos moved to `%s` with its [%s](%s), but this branch has commits from someone else, so I've left it alone. "+
		"Run `make sync-protos` and the codegen here to catch up, or close this and delete the branch, and I'll open a fresh one.\n\n%s",
		in.BSR.Short(), pugCommit(in.BSR), in.BSR.SourceControlURL, gh.Marker(gh.MarkerProtosLocked, in.BSR.Commit))
	return c.Comment(ctx, in.Repo, pr.Number, body)
}

func ensurePR(ctx context.Context, c *gh.Client, in SyncInput, pr *gh.PR, changes []gh.Change, replaced int) error {
	title, body := PRTitle(in.BSR), PRBody(in.BSR, changes)
	if pr != nil {
		if pr.Title == title && pr.Body == body {
			return nil
		}
		return c.EditPR(ctx, in.Repo, pr.Number, title, body)
	}
	number, err := c.CreatePR(ctx, in.Repo, title, body, gh.SyncBranch, in.Branch)
	if err != nil || replaced == 0 {
		return err
	}
	successor := fmt.Sprintf("#%d", number)
	if number == 0 {
		successor = "the new pull request"
	}
	return c.Comment(ctx, in.Repo, replaced, fmt.Sprintf("Replaced by %s: %s changed .github/workflows/ since this branch's base, and I can't move a branch past workflow changes.", successor, in.Branch))
}

func PRTitle(b BSRCommit) string { return "Sync protos to " + b.Short() }

func PRBody(b BSRCommit, changes []gh.Change) string {
	var protos []string
	generated := 0
	for _, ch := range changes {
		if !strings.HasPrefix(ch.Path, "proto/") {
			generated++
			continue
		}
		line := "- `" + ch.Path + "`"
		if ch.Deleted {
			line += " (removed)"
		}
		protos = append(protos, line)
	}
	var s strings.Builder
	fmt.Fprintf(&s, "Syncs `proto/` to `%s:%s`, from pug's [%s](%s), and regenerates the code.\n\n", Module, b.Commit, pugCommit(b), b.SourceControlURL)
	if len(protos) > 0 {
		fmt.Fprintf(&s, "**Changed protos**\n\n%s\n\n", strings.Join(protos, "\n"))
	}
	if generated > 0 {
		fmt.Fprintf(&s, "With them, %d other files changed: the Makefile's pin and the generated code.\n\n", generated)
	}
	s.WriteString("I'll update this branch when pug's protos change again.\n\n")
	s.WriteString(gh.Marker(gh.MarkerProtos, b.Commit))
	return s.String()
}

func commitMessage(b BSRCommit) string {
	return fmt.Sprintf("Sync protos to %s\n\n%s:%s, from %s.\n", b.Short(), Module, b.Commit, b.SourceControlURL)
}

// applyPatch stages the codegen job's diff in dir and returns what it changes. The diff comes
// from a job that ran third-party code, so it may not touch .github/.
func applyPatch(ctx context.Context, dir, patch string) ([]gh.Change, error) {
	info, err := os.Stat(patch)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, nil
	}
	abs, err := filepath.Abs(patch)
	if err != nil {
		return nil, err
	}
	if _, err := git.Run(ctx, dir, "apply", "--index", abs); err != nil {
		return nil, err
	}
	out, err := git.Run(ctx, dir, "diff", "--cached", "--raw", "--no-renames", "--no-abbrev", "-z")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var changes []gh.Change
	for i := 0; i+1 < len(fields); i += 2 {
		meta, path := strings.Fields(strings.TrimPrefix(fields[i], ":")), fields[i+1]
		if len(meta) != 5 {
			return nil, fmt.Errorf("git diff --raw: malformed %q", fields[i])
		}
		if strings.HasPrefix(path, ".github/") {
			return nil, fmt.Errorf("the codegen job changed %s, which a proto sync never does", path)
		}
		oldMode, newMode, newSHA, status := meta[0], meta[1], meta[3], meta[4]
		switch status {
		case "D":
			changes = append(changes, gh.Change{Path: path, Mode: oldMode, Deleted: true})
		case "A", "M", "T":
			if newMode == "160000" {
				return nil, fmt.Errorf("the codegen job changed the submodule %s", path)
			}
			content, err := git.Run(ctx, dir, "cat-file", "blob", newSHA)
			if err != nil {
				return nil, err
			}
			changes = append(changes, gh.Change{Path: path, Mode: newMode, Content: content})
		default:
			return nil, fmt.Errorf("git diff --raw: unexpected status %s for %s", status, path)
		}
	}
	return changes, nil
}
