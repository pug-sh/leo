package gh

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-github/v93/github"
)

// Change is one file a commit adds, modifies or deletes.
type Change struct {
	Path    string
	Mode    string
	Content []byte
	Deleted bool
}

// Branch returns the commit a branch points at, or "" if it doesn't exist.
func (c *Client) Branch(ctx context.Context, repo, branch string) (string, error) {
	ref, _, err := c.rest.Git.GetRef(ctx, Org, repo, "heads/"+branch)
	if isStatus(err, 404) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s: read branch %s: %w", repo, branch, err)
	}
	return ref.GetObject().GetSHA(), nil
}

// CommitTree returns the tree of a commit.
func (c *Client) CommitTree(ctx context.Context, repo, sha string) (string, error) {
	commit, _, err := c.rest.Git.GetCommit(ctx, Org, repo, sha)
	if err != nil {
		return "", fmt.Errorf("%s: read commit %s: %w", repo, sha, err)
	}
	return commit.GetTree().GetSHA(), nil
}

// SubtreeSHA returns the tree a commit has at dir, a directory at its root, or "" if it has none.
func (c *Client) SubtreeSHA(ctx context.Context, repo, commit, dir string) (string, error) {
	root, err := c.CommitTree(ctx, repo, commit)
	if err != nil {
		return "", err
	}
	tree, _, err := c.rest.Git.GetTree(ctx, Org, repo, root, false)
	if err != nil {
		return "", fmt.Errorf("%s: read tree %s: %w", repo, root, err)
	}
	for _, e := range tree.Entries {
		if e.GetPath() == dir && e.GetType() == "tree" {
			return e.GetSHA(), nil
		}
	}
	return "", nil
}

// CommitsByOthers reports whether head has commits that base doesn't and that Leo didn't author.
func (c *Client) CommitsByOthers(ctx context.Context, repo, base, head string) (bool, error) {
	cmp, _, err := c.rest.Repositories.CompareCommits(ctx, Org, repo, base, head, &github.ListOptions{PerPage: 250})
	if err != nil {
		return false, fmt.Errorf("%s: compare %s...%s: %w", repo, base, head, err)
	}
	for _, commit := range cmp.Commits {
		if commit.GetAuthor().GetLogin() != BotLogin {
			return true, nil
		}
	}
	return false, nil
}

// CreateTree writes changes on top of the tree base, blob by blob, and returns the new tree. In
// a dry run it returns "".
func (c *Client) CreateTree(ctx context.Context, repo, base string, changes []Change) (string, error) {
	if !c.write("%s: write %d changed files on top of tree %s", repo, len(changes), short(base)) {
		return "", nil
	}
	var entries []*github.TreeEntry
	for _, ch := range changes {
		entry := &github.TreeEntry{Path: new(ch.Path), Mode: new(ch.Mode), Type: new("blob")}
		if !ch.Deleted {
			blob, _, err := c.rest.Git.CreateBlob(ctx, Org, repo, github.Blob{
				Content:  base64.StdEncoding.EncodeToString(ch.Content),
				Encoding: new("base64"),
			})
			if err != nil {
				return "", fmt.Errorf("%s: write %s: %w", repo, ch.Path, err)
			}
			entry.SHA = blob.SHA
		}
		entries = append(entries, entry)
	}
	tree, _, err := c.rest.Git.CreateTree(ctx, Org, repo, base, entries)
	if err != nil {
		return "", fmt.Errorf("%s: write tree: %w", repo, err)
	}
	return tree.GetSHA(), nil
}

// CreateCommit commits tree on top of parent. Leo signs it, since the API makes it.
func (c *Client) CreateCommit(ctx context.Context, repo, message, tree, parent string) (string, error) {
	subject, _, _ := strings.Cut(message, "\n")
	if !c.write("%s: commit %q on top of %s", repo, subject, short(parent)) {
		return "", nil
	}
	commit, _, err := c.rest.Git.CreateCommit(ctx, Org, repo, github.Commit{
		Message: new(message),
		Tree:    &github.Tree{SHA: new(tree)},
		Parents: []*github.Commit{{SHA: new(parent)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("%s: commit: %w", repo, err)
	}
	return commit.GetSHA(), nil
}

// ErrWorkflowRefused is what GitHub answers when a branch update would carry changes to
// .github/workflows/, which the App has no permission for.
var ErrWorkflowRefused = errors.New("refused: the update carries workflow changes, and Leo has no Workflows permission")

func (c *Client) CreateBranch(ctx context.Context, repo, branch, sha string) error {
	if !c.write("%s: create branch %s at %s", repo, branch, commitLabel(sha)) {
		return nil
	}
	_, _, err := c.rest.Git.CreateRef(ctx, Org, repo, github.CreateRef{Ref: "refs/heads/" + branch, SHA: sha})
	if err != nil {
		return fmt.Errorf("%s: create branch %s: %w", repo, branch, refusal(err))
	}
	return nil
}

func (c *Client) ForceBranch(ctx context.Context, repo, branch, sha string) error {
	if !c.write("%s: force %s to %s", repo, branch, commitLabel(sha)) {
		return nil
	}
	_, _, err := c.rest.Git.UpdateRef(ctx, Org, repo, "heads/"+branch, github.UpdateRef{SHA: sha, Force: new(true)})
	if err != nil {
		return fmt.Errorf("%s: force %s: %w", repo, branch, refusal(err))
	}
	return nil
}

func (c *Client) DeleteBranch(ctx context.Context, repo, branch string) error {
	if !c.write("%s: delete branch %s", repo, branch) {
		return nil
	}
	if _, err := c.rest.Git.DeleteRef(ctx, Org, repo, "heads/"+branch); err != nil {
		return fmt.Errorf("%s: delete branch %s: %w", repo, branch, err)
	}
	return nil
}

func refusal(err error) error {
	if isStatus(err, 403, 422) && strings.Contains(strings.ToLower(err.Error()), "workflow") {
		return fmt.Errorf("%w: %w", ErrWorkflowRefused, err)
	}
	return err
}

// commitLabel names a commit that a dry run only pretended to make.
func commitLabel(sha string) string {
	if sha == "" {
		return "the new commit"
	}
	return short(sha)
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
