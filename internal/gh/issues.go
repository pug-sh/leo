package gh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"
)

type Issue struct {
	Number   int
	Title    string
	Body     string
	Open     bool
	ClosedAt time.Time
	URL      string
}

// LeoIssues lists the issues Leo opened in repo that carry marker, newest first.
func (c *Client) LeoIssues(ctx context.Context, repo, marker string) ([]Issue, error) {
	opts := &github.IssueListByRepoOptions{Creator: BotLogin, State: "all", Sort: "created", Direction: "desc", ListOptions: github.ListOptions{PerPage: 100}}
	var issues []Issue
	for {
		page, resp, err := c.rest.Issues.ListByRepo(ctx, Org, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: list Leo's issues: %w", repo, err)
		}
		for _, is := range page {
			if is.IsPullRequest() || is.GetUser().GetLogin() != BotLogin || !HasMarker(is.GetBody(), marker) {
				continue
			}
			issues = append(issues, Issue{
				Number:   is.GetNumber(),
				Title:    is.GetTitle(),
				Body:     is.GetBody(),
				Open:     is.GetState() == "open",
				ClosedAt: is.GetClosedAt().Time,
				URL:      is.GetHTMLURL(),
			})
		}
		if resp.NextPage == 0 {
			return issues, nil
		}
		opts.ListOptions.Page = resp.NextPage
	}
}

// ClosedByPerson reports whether someone other than Leo closed the issue.
func (c *Client) ClosedByPerson(ctx context.Context, repo string, number int) (bool, error) {
	is, _, err := c.rest.Issues.Get(ctx, Org, repo, number)
	if err != nil {
		return false, fmt.Errorf("%s#%d: %w", repo, number, err)
	}
	return is.GetState() == "closed" && is.GetClosedBy().GetLogin() != BotLogin, nil
}

func (c *Client) CreateIssue(ctx context.Context, repo, title, body string) (int, error) {
	if !c.write("%s: open issue %q", repo, title) {
		c.detail(body)
		return 0, nil
	}
	is, _, err := c.rest.Issues.Create(ctx, Org, repo, github.CreateIssueRequest{Title: title, Body: &body})
	if err != nil {
		return 0, fmt.Errorf("%s: open issue %q: %w", repo, title, err)
	}
	c.Logf("%s", is.GetHTMLURL())
	return is.GetNumber(), nil
}

func (c *Client) EditIssue(ctx context.Context, repo string, number int, title, body string) error {
	if !c.write("%s#%d: update to %q", repo, number, title) {
		c.detail(body)
		return nil
	}
	if _, _, err := c.rest.Issues.Update(ctx, Org, repo, number, github.UpdateIssueRequest{Title: &title, Body: &body}); err != nil {
		return fmt.Errorf("%s#%d: update: %w", repo, number, err)
	}
	return nil
}

func (c *Client) Comment(ctx context.Context, repo string, number int, body string) error {
	first, rest, multiline := strings.Cut(body, "\n")
	if !c.write("%s#%d: comment %q", repo, number, first) {
		if multiline {
			c.detail(strings.TrimLeft(rest, "\n"))
		}
		return nil
	}
	if _, _, err := c.rest.Issues.CreateComment(ctx, Org, repo, number, github.IssueCommentRequest{Body: body}); err != nil {
		return fmt.Errorf("%s#%d: comment: %w", repo, number, err)
	}
	return nil
}

func (c *Client) CloseIssue(ctx context.Context, repo string, number int, comment string) error {
	if err := c.Comment(ctx, repo, number, comment); err != nil {
		return err
	}
	if !c.write("%s#%d: close", repo, number) {
		return nil
	}
	req := github.UpdateIssueRequest{State: new("closed"), StateReason: new("completed")}
	if _, _, err := c.rest.Issues.Update(ctx, Org, repo, number, req); err != nil {
		return fmt.Errorf("%s#%d: close: %w", repo, number, err)
	}
	return nil
}

// detail prints text under a dry-run line, so a dry run shows what Leo would write.
func (c *Client) detail(text string) {
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(c.out, "    | %s\n", line)
	}
}
