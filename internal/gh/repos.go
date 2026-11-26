package gh

import (
	"context"
	"slices"
	"sort"

	"github.com/google/go-github/v93/github"
)

type Repo struct {
	Name          string
	DefaultBranch string
	Topics        []string
}

func (r Repo) HasTopic(topic string) bool {
	return slices.Contains(r.Topics, topic)
}

// Repos lists the repos in scope: public, not archived and not forks.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	opts := &github.RepositoryListByOrgOptions{Type: "public", PerPage: 100}
	var repos []Repo
	for {
		page, resp, err := c.rest.Repositories.ListByOrg(ctx, Org, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.GetArchived() || r.GetFork() || r.GetPrivate() || r.GetVisibility() != "public" {
				continue
			}
			repos = append(repos, Repo{Name: r.GetName(), DefaultBranch: r.GetDefaultBranch(), Topics: r.Topics})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos, nil
}
