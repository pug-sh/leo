package gh

import (
	"context"
	"fmt"
	"time"

	"github.com/shurcooL/githubv4"
)

// Content is an open issue or pull request.
type Content struct {
	ID          string
	Repo        string
	Number      int
	PR          bool
	Draft       bool
	Title       string
	Body        string
	Author      string
	Bot         bool
	Association string
	HeadRef     string
	// Checks is the head commit's combined check and status state, empty when it has none.
	Checks     string
	ReopenedAt time.Time
}

func (c Content) Label() string { return fmt.Sprintf("%s#%d", c.Repo, c.Number) }

type actor struct {
	Login    string
	Typename string `graphql:"__typename"`
}

type reopened struct {
	Nodes []struct {
		ReopenedEvent struct {
			CreatedAt time.Time
		} `graphql:"... on ReopenedEvent"`
	}
}

func (r reopened) at() time.Time {
	if len(r.Nodes) == 0 {
		return time.Time{}
	}
	return r.Nodes[0].ReopenedEvent.CreatedAt
}

func (c *Client) OpenContent(ctx context.Context, repo string) ([]Content, error) {
	issues, err := c.openIssues(ctx, repo)
	if err != nil {
		return nil, err
	}
	prs, err := c.openPRs(ctx, repo)
	if err != nil {
		return nil, err
	}
	return append(issues, prs...), nil
}

func (c *Client) openIssues(ctx context.Context, repo string) ([]Content, error) {
	var q struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ID                string
					Number            int
					Title             string
					Body              string
					Author            actor
					AuthorAssociation string
					TimelineItems     reopened `graphql:"timelineItems(itemTypes: [REOPENED_EVENT], last: 1)"`
				}
				PageInfo pageInfo
			} `graphql:"issues(states: OPEN, first: 100, after: $cursor)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]any{"owner": githubv4.String(Org), "name": githubv4.String(repo), "cursor": (*githubv4.String)(nil)}
	var out []Content
	for {
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, fmt.Errorf("%s: list open issues: %w", repo, err)
		}
		for _, n := range q.Repository.Issues.Nodes {
			out = append(out, Content{
				ID:          n.ID,
				Repo:        repo,
				Number:      n.Number,
				Title:       n.Title,
				Body:        n.Body,
				Author:      NormalizeLogin(n.Author.Login, n.Author.Typename),
				Bot:         n.Author.Typename == "Bot",
				Association: n.AuthorAssociation,
				ReopenedAt:  n.TimelineItems.at(),
			})
		}
		if !q.Repository.Issues.PageInfo.HasNextPage {
			return out, nil
		}
		vars["cursor"] = cursor(q.Repository.Issues.PageInfo)
	}
}

func (c *Client) openPRs(ctx context.Context, repo string) ([]Content, error) {
	var q struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					ID                string
					Number            int
					Title             string
					Body              string
					Author            actor
					AuthorAssociation string
					IsDraft           bool
					HeadRefName       string
					StatusCheckRollup *struct {
						State string
					}
					TimelineItems reopened `graphql:"timelineItems(itemTypes: [REOPENED_EVENT], last: 1)"`
				}
				PageInfo pageInfo
			} `graphql:"pullRequests(states: OPEN, first: 100, after: $cursor)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]any{"owner": githubv4.String(Org), "name": githubv4.String(repo), "cursor": (*githubv4.String)(nil)}
	var out []Content
	for {
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, fmt.Errorf("%s: list open pull requests: %w", repo, err)
		}
		for _, n := range q.Repository.PullRequests.Nodes {
			ct := Content{
				ID:          n.ID,
				Repo:        repo,
				Number:      n.Number,
				PR:          true,
				Draft:       n.IsDraft,
				Title:       n.Title,
				Body:        n.Body,
				Author:      NormalizeLogin(n.Author.Login, n.Author.Typename),
				Bot:         n.Author.Typename == "Bot",
				Association: n.AuthorAssociation,
				HeadRef:     n.HeadRefName,
				ReopenedAt:  n.TimelineItems.at(),
			}
			if n.StatusCheckRollup != nil {
				ct.Checks = n.StatusCheckRollup.State
			}
			out = append(out, ct)
		}
		if !q.Repository.PullRequests.PageInfo.HasNextPage {
			return out, nil
		}
		vars["cursor"] = cursor(q.Repository.PullRequests.PageInfo)
	}
}
