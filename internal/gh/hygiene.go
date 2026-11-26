package gh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-github/v93/github"
)

// ErrForbidden means Leo's token lacks the permission a read needs.
var ErrForbidden = errors.New("forbidden")

// RootEntries lists the names of the files and directories at the root of repo's default branch.
func (c *Client) RootEntries(ctx context.Context, repo string) ([]string, error) {
	_, dir, _, err := c.rest.Repositories.GetContents(ctx, Org, repo, "", nil)
	if err != nil {
		return nil, fmt.Errorf("%s: list root: %w", repo, err)
	}
	names := make([]string, 0, len(dir))
	for _, e := range dir {
		names = append(names, e.GetName())
	}
	return names, nil
}

type ActionsWorkflow struct {
	Name  string
	Path  string
	State string
}

func (c *Client) ActionsWorkflows(ctx context.Context, repo string) ([]ActionsWorkflow, error) {
	opts := &github.ListOptions{PerPage: 100}
	var workflows []ActionsWorkflow
	for {
		page, resp, err := c.rest.Actions.ListWorkflows(ctx, Org, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: list workflows: %w", repo, err)
		}
		for _, w := range page.Workflows {
			workflows = append(workflows, ActionsWorkflow{Name: w.GetName(), Path: w.GetPath(), State: w.GetState()})
		}
		if resp.NextPage == 0 {
			return workflows, nil
		}
		opts.Page = resp.NextPage
	}
}

// File returns a file on repo's default branch, or nil if there is none.
func (c *Client) File(ctx context.Context, repo, path string) ([]byte, error) {
	file, _, _, err := c.rest.Repositories.GetContents(ctx, Org, repo, path, nil)
	if isStatus(err, 404) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", repo, path, err)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", repo, path, err)
	}
	return []byte(content), nil
}

// DependabotAlertsOn reports whether repo has Dependabot alerts turned on.
func (c *Client) DependabotAlertsOn(ctx context.Context, repo string) (bool, error) {
	_, _, err := c.rest.Dependabot.ListRepoAlerts(ctx, Org, repo, &github.ListAlertsOptions{ListCursorOptions: github.ListCursorOptions{PerPage: 1}})
	var e *github.ErrorResponse
	if isStatus(err, 403) && errors.As(err, &e) && strings.Contains(e.Message, "disabled") {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: read Dependabot alerts: %w", repo, err)
	}
	return true, nil
}

// SecurityUpdatesOn reports whether Dependabot opens security updates in repo. Reading it takes the
// Administration permission, without which it returns ErrForbidden.
func (c *Client) SecurityUpdatesOn(ctx context.Context, repo string) (bool, error) {
	fixes, _, err := c.rest.Repositories.GetAutomatedSecurityFixes(ctx, Org, repo)
	if isStatus(err, 403) {
		return false, ErrForbidden
	}
	if err != nil {
		return false, fmt.Errorf("%s: read security updates: %w", repo, err)
	}
	return fixes.GetEnabled(), nil
}
