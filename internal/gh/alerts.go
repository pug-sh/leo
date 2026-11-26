package gh

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// DependabotUpdate is what Dependabot did about one open alert.
type DependabotUpdate struct {
	Alert   int
	PR      int
	PRState string
	PRURL   string
	Error   string
}

// OpenPR reports whether Dependabot has an open pull request for the alert.
func (u DependabotUpdate) OpenPR() bool { return u.PR != 0 && u.PRState == "OPEN" }

// DependabotUpdates reads, for each open Dependabot alert, the pull request Dependabot opened for
// it. REST doesn't link alerts to pull requests.
func (c *Client) DependabotUpdates(ctx context.Context, repo string) (map[int]DependabotUpdate, error) {
	var q struct {
		Repository struct {
			VulnerabilityAlerts struct {
				Nodes []struct {
					Number           int
					DependabotUpdate *struct {
						PullRequest *struct {
							Number int
							State  string
							URL    string
						}
						Error *struct {
							Title string
						}
					}
				}
				PageInfo pageInfo
			} `graphql:"vulnerabilityAlerts(states: [OPEN], first: 100, after: $cursor)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]any{"owner": githubv4.String(Org), "name": githubv4.String(repo), "cursor": (*githubv4.String)(nil)}
	updates := map[int]DependabotUpdate{}
	for {
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, fmt.Errorf("%s: read Dependabot updates: %w", repo, err)
		}
		for _, n := range q.Repository.VulnerabilityAlerts.Nodes {
			u := DependabotUpdate{Alert: n.Number}
			if d := n.DependabotUpdate; d != nil {
				if d.PullRequest != nil {
					u.PR, u.PRState, u.PRURL = d.PullRequest.Number, d.PullRequest.State, d.PullRequest.URL
				}
				if d.Error != nil {
					u.Error = d.Error.Title
				}
			}
			updates[n.Number] = u
		}
		if !q.Repository.VulnerabilityAlerts.PageInfo.HasNextPage {
			return updates, nil
		}
		vars["cursor"] = cursor(q.Repository.VulnerabilityAlerts.PageInfo)
	}
}
