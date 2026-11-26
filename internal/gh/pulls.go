package gh

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/go-github/v93/github"
)

type PR struct {
	Number  int
	Title   string
	Body    string
	URL     string
	Author  string
	HeadRef string
	HeadSHA string
}

func toPR(pr *github.PullRequest) PR {
	return PR{
		Number:  pr.GetNumber(),
		Title:   pr.GetTitle(),
		Body:    pr.GetBody(),
		URL:     pr.GetHTMLURL(),
		Author:  pr.GetUser().GetLogin(),
		HeadRef: pr.GetHead().GetRef(),
		HeadSHA: pr.GetHead().GetSHA(),
	}
}

// OpenPRs lists repo's open pull requests.
func (c *Client) OpenPRs(ctx context.Context, repo string) ([]PR, error) {
	opts := &github.PullRequestListOptions{State: "open", ListOptions: github.ListOptions{PerPage: 100}}
	var prs []PR
	for {
		page, resp, err := c.rest.PullRequests.List(ctx, Org, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: list open pull requests: %w", repo, err)
		}
		for _, pr := range page {
			prs = append(prs, toPR(pr))
		}
		if resp.NextPage == 0 {
			return prs, nil
		}
		opts.Page = resp.NextPage
	}
}

// ClosePR comments on a pull request and closes it.
func (c *Client) ClosePR(ctx context.Context, repo string, number int, comment string) error {
	if err := c.Comment(ctx, repo, number, comment); err != nil {
		return err
	}
	if !c.write("%s#%d: close", repo, number) {
		return nil
	}
	if _, _, err := c.rest.PullRequests.Edit(ctx, Org, repo, number, &github.PullRequest{State: new("closed")}); err != nil {
		return fmt.Errorf("%s#%d: close: %w", repo, number, err)
	}
	return nil
}

// OpenPR returns the open pull request from branch, or nil if there is none.
func (c *Client) OpenPR(ctx context.Context, repo, branch string) (*PR, error) {
	opts := &github.PullRequestListOptions{State: "open", Head: Org + ":" + branch}
	prs, _, err := c.rest.PullRequests.List(ctx, Org, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: list pull requests from %s: %w", repo, branch, err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	pr := toPR(prs[0])
	return &pr, nil
}

// LastClosedPR returns the newest closed pull request from branch, and whether it was merged.
func (c *Client) LastClosedPR(ctx context.Context, repo, branch string) (*PR, bool, error) {
	opts := &github.PullRequestListOptions{State: "closed", Head: Org + ":" + branch, Sort: "created", Direction: "desc"}
	prs, _, err := c.rest.PullRequests.List(ctx, Org, repo, opts)
	if err != nil {
		return nil, false, fmt.Errorf("%s: list closed pull requests from %s: %w", repo, branch, err)
	}
	if len(prs) == 0 {
		return nil, false, nil
	}
	pr := toPR(prs[0])
	return &pr, prs[0].MergedAt != nil, nil
}

func (c *Client) CreatePR(ctx context.Context, repo, title, body, head, base string) (int, error) {
	if !c.write("%s: open pull request %q from %s", repo, title, head) {
		c.detail(body)
		return 0, nil
	}
	pr, _, err := c.rest.PullRequests.Create(ctx, Org, repo, github.CreatePullRequest{Title: &title, Body: &body, Head: head, Base: base})
	if err != nil {
		return 0, fmt.Errorf("%s: open pull request %q: %w", repo, title, err)
	}
	c.Logf("%s", pr.GetHTMLURL())
	return pr.GetNumber(), nil
}

func (c *Client) EditPR(ctx context.Context, repo string, number int, title, body string) error {
	if !c.write("%s#%d: update to %q", repo, number, title) {
		c.detail(body)
		return nil
	}
	if _, _, err := c.rest.PullRequests.Edit(ctx, Org, repo, number, &github.PullRequest{Title: &title, Body: &body}); err != nil {
		return fmt.Errorf("%s#%d: update: %w", repo, number, err)
	}
	return nil
}

// LeoCommented reports whether Leo already commented on an issue or pull request with a marker
// called name and these arguments.
func (c *Client) LeoCommented(ctx context.Context, repo string, number int, name string, args ...string) (bool, error) {
	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := c.rest.Issues.ListComments(ctx, Org, repo, number, opts)
		if err != nil {
			return false, fmt.Errorf("%s#%d: list comments: %w", repo, number, err)
		}
		for _, cm := range comments {
			if cm.GetUser().GetLogin() != BotLogin {
				continue
			}
			if got, ok := ParseMarker(cm.GetBody(), name); ok && slices.Equal(got, args) {
				return true, nil
			}
		}
		if resp.NextPage == 0 {
			return false, nil
		}
		opts.Page = resp.NextPage
	}
}
