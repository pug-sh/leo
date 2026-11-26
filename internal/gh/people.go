package gh

import (
	"context"
	"fmt"
)

// IsTeam reports whether login is an org member or a collaborator on repo.
func (c *Client) IsTeam(ctx context.Context, repo, login string) (bool, error) {
	member, ok := c.members[login]
	if !ok {
		var err error
		member, _, err = c.rest.Organizations.IsMember(ctx, Org, login)
		if err != nil {
			return false, fmt.Errorf("check whether %s is a member of %s: %w", login, Org, err)
		}
		c.members[login] = member
	}
	if member {
		return true, nil
	}
	key := repo + "/" + login
	collaborator, ok := c.team[key]
	if !ok {
		var err error
		collaborator, _, err = c.rest.Repositories.IsCollaborator(ctx, Org, repo, login)
		if err != nil {
			return false, fmt.Errorf("check whether %s collaborates on %s: %w", login, repo, err)
		}
		c.team[key] = collaborator
	}
	return collaborator, nil
}
