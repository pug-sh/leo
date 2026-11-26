package gh

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/shurcooL/githubv4"
)

type DependabotAlert struct {
	Number    int
	GHSA      string
	CVE       string
	Package   string
	Ecosystem string
	Manifest  string
	Severity  string
	Fixed     string
	URL       string
	Created   time.Time
}

// DependabotAlerts lists repo's open Dependabot alerts. A repo with Dependabot alerts turned off
// has none.
func (c *Client) DependabotAlerts(ctx context.Context, repo string) ([]DependabotAlert, error) {
	opts := &github.ListAlertsOptions{State: new("open"), ListCursorOptions: github.ListCursorOptions{PerPage: 100}}
	var alerts []DependabotAlert
	for {
		page, resp, err := c.rest.Dependabot.ListRepoAlerts(ctx, Org, repo, opts)
		if err != nil {
			var e *github.ErrorResponse
			if isStatus(err, 403) && errors.As(err, &e) && strings.Contains(e.Message, "disabled") {
				return nil, nil
			}
			return nil, fmt.Errorf("%s: list Dependabot alerts: %w", repo, err)
		}
		for _, a := range page {
			alerts = append(alerts, DependabotAlert{
				Number:    a.GetNumber(),
				GHSA:      a.GetSecurityAdvisory().GetGHSAID(),
				CVE:       a.GetSecurityAdvisory().GetCVEID(),
				Package:   a.GetDependency().GetPackage().GetName(),
				Ecosystem: a.GetDependency().GetPackage().GetEcosystem(),
				Manifest:  a.GetDependency().GetManifestPath(),
				Severity:  a.GetSecurityAdvisory().GetSeverity(),
				Fixed:     a.GetSecurityVulnerability().GetFirstPatchedVersion().GetIdentifier(),
				URL:       a.GetHTMLURL(),
				Created:   a.GetCreatedAt().Time,
			})
		}
		if resp.After == "" {
			return alerts, nil
		}
		opts.ListCursorOptions.After = resp.After
	}
}

type CodeScanningAlert struct {
	Number   int
	Rule     string
	Severity string
	Path     string
	Message  string
	URL      string
	Created  time.Time
}

// CodeScanningAlerts lists repo's open code scanning alerts from tool. A repo without code
// scanning has none.
func (c *Client) CodeScanningAlerts(ctx context.Context, repo, tool string) ([]CodeScanningAlert, error) {
	opts := &github.AlertListOptions{State: "open", ToolName: tool, ListOptions: github.ListOptions{PerPage: 100}}
	var alerts []CodeScanningAlert
	for {
		page, resp, err := c.rest.CodeScanning.ListAlertsForRepo(ctx, Org, repo, opts)
		if err != nil {
			if isStatus(err, 404) {
				return nil, nil
			}
			return nil, fmt.Errorf("%s: list code scanning alerts: %w", repo, err)
		}
		for _, a := range page {
			severity := a.GetRule().GetSecuritySeverityLevel()
			if severity == "" {
				severity = a.GetRule().GetSeverity()
			}
			alerts = append(alerts, CodeScanningAlert{
				Number:   a.GetNumber(),
				Rule:     a.GetRule().GetID(),
				Severity: severity,
				Path:     a.GetMostRecentInstance().GetLocation().GetPath(),
				Message:  a.GetMostRecentInstance().GetMessage().GetText(),
				URL:      a.GetHTMLURL(),
				Created:  a.GetCreatedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			return alerts, nil
		}
		opts.ListOptions.Page = resp.NextPage
	}
}

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
