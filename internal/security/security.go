package security

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pug-sh/leo/internal/gh"
)

// wait gives Dependabot time to open its pull request before Leo opens an issue.
const wait = time.Hour

// Run opens an issue for every advisory with open alerts that no pull request covers, and closes
// Leo's issues once pull requests cover their alerts or no open alert remains.
func Run(ctx context.Context, c *gh.Client, osv *OSV, now time.Time) error {
	repos, err := c.Repos(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range repos {
		if err := runRepo(ctx, c, osv, r.Name, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func runRepo(ctx context.Context, c *gh.Client, osv *OSV, repo string, now time.Time) error {
	alerts, err := loadAlerts(ctx, c, osv, repo)
	if err != nil {
		return err
	}
	issues, err := c.LeoIssues(ctx, repo, gh.MarkerSecurity)
	if err != nil {
		return err
	}
	handled := map[int]bool{}
	var errs []error
	for _, g := range GroupAlerts(alerts) {
		if err := handleGroup(ctx, c, repo, g, alerts, issues, handled, now); err != nil {
			errs = append(errs, err)
		}
	}
	for _, is := range issues {
		if !is.Open || handled[is.Number] {
			continue
		}
		id := markerID(is)
		if err := c.CloseIssue(ctx, repo, is.Number, fmt.Sprintf("No open alert remains for %s, so I'm closing this.", id)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func loadAlerts(ctx context.Context, c *gh.Client, osv *OSV, repo string) ([]Alert, error) {
	dependabot, err := c.DependabotAlerts(ctx, repo)
	if err != nil {
		return nil, err
	}
	var alerts []Alert
	if len(dependabot) > 0 {
		updates, err := c.DependabotUpdates(ctx, repo)
		if err != nil {
			return nil, err
		}
		for _, a := range dependabot {
			alerts = append(alerts, fromDependabot(a, updates[a.Number]))
		}
	}
	scans, err := c.CodeScanningAlerts(ctx, repo, osvTool)
	if err != nil {
		return nil, err
	}
	for _, s := range scans {
		aliases, err := osv.Aliases(ctx, s.Rule)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, fromOSV(s, aliases))
	}
	return alerts, nil
}

func handleGroup(ctx context.Context, c *gh.Client, repo string, g Group, all []Alert, issues []gh.Issue, handled map[int]bool, now time.Time) error {
	prs, covered := Coverage(g, all)
	var open, last *gh.Issue
	for i := range issues {
		if !g.Has(markerID(issues[i])) {
			continue
		}
		if last == nil {
			last = &issues[i]
		}
		if issues[i].Open {
			open = &issues[i]
			break
		}
	}

	if open != nil {
		handled[open.Number] = true
		if covered {
			return c.CloseIssue(ctx, repo, open.Number, fmt.Sprintf("%s every alert for %s, so I'm closing this.", cover(prs), g.ID))
		}
		title, body := issueTitle(g), issueBody(repo, g, prs)
		if open.Title == title && open.Body == body {
			return nil
		}
		return c.EditIssue(ctx, repo, open.Number, title, body)
	}
	if covered || now.Sub(g.oldest()) < wait {
		return nil
	}
	if last != nil {
		// Someone closed Leo's last issue for this advisory: only a new alert brings it back.
		byPerson, err := c.ClosedByPerson(ctx, repo, last.Number)
		if err != nil {
			return err
		}
		if byPerson && !g.newest().After(last.ClosedAt) {
			return nil
		}
	}
	_, err := c.CreateIssue(ctx, repo, issueTitle(g), issueBody(repo, g, prs))
	return err
}

func markerID(is gh.Issue) string {
	args, _ := gh.ParseMarker(is.Body, gh.MarkerSecurity)
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func cover(prs []int) string {
	refs := make([]string, len(prs))
	for i, pr := range prs {
		refs[i] = fmt.Sprintf("#%d", pr)
	}
	if len(refs) == 1 {
		return refs[0] + " covers"
	}
	return strings.Join(refs[:len(refs)-1], ", ") + " and " + refs[len(refs)-1] + " cover"
}

func issueTitle(g Group) string {
	return fmt.Sprintf("Security: %s in %s", g.ID, strings.Join(packages(g), ", "))
}

func packages(g Group) []string {
	var pkgs []string
	for _, a := range g.Alerts {
		if a.Package != "" && !slices.Contains(pkgs, a.Package) {
			pkgs = append(pkgs, a.Package)
		}
	}
	slices.Sort(pkgs)
	if len(pkgs) == 0 {
		return []string{"a dependency"}
	}
	return pkgs
}

func advisoryURL(id string) string {
	if strings.HasPrefix(id, "GHSA-") {
		return "https://github.com/advisories/" + id
	}
	return "https://osv.dev/vulnerability/" + id
}

func issueBody(repo string, g Group, prs []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Open alerts in %s for [%s](%s) that no pull request fixes yet.\n\n", repo, g.ID, advisoryURL(g.ID))
	if s := g.severity(); s != "" {
		fmt.Fprintf(&b, "- **Severity:** %s\n", s)
	}
	var pkgs []string
	for _, p := range packages(g) {
		pkgs = append(pkgs, "`"+p+"`")
	}
	fmt.Fprintf(&b, "- **Package:** %s\n", strings.Join(pkgs, ", "))
	var fixed, manifests, aka []string
	for _, a := range g.Alerts {
		if a.Fixed != "" && !slices.Contains(fixed, a.Fixed) {
			fixed = append(fixed, a.Fixed)
		}
		if a.Manifest != "" && !slices.Contains(manifests, "`"+a.Manifest+"`") {
			manifests = append(manifests, "`"+a.Manifest+"`")
		}
	}
	for _, id := range g.IDs {
		if id != g.ID {
			aka = append(aka, id)
		}
	}
	slices.Sort(fixed)
	slices.Sort(manifests)
	if len(fixed) > 0 {
		fmt.Fprintf(&b, "- **Fixed in:** %s\n", strings.Join(fixed, ", "))
	}
	if len(manifests) > 0 {
		fmt.Fprintf(&b, "- **Manifests:** %s\n", strings.Join(manifests, ", "))
	}
	if len(aka) > 0 {
		fmt.Fprintf(&b, "- **Also known as:** %s\n", strings.Join(aka, ", "))
	}

	b.WriteString("\n**Alerts**\n\n")
	var errs []string
	for _, a := range g.Alerts {
		fmt.Fprintf(&b, "- [%s alert #%d](%s)", a.Source, a.Number, a.URL)
		if a.PR != 0 {
			fmt.Fprintf(&b, ", fixed by #%d", a.PR)
		}
		b.WriteString("\n")
		if a.UpdateError != "" && !slices.Contains(errs, a.UpdateError) {
			errs = append(errs, a.UpdateError)
		}
	}
	b.WriteString("\n")
	switch {
	case len(prs) > 0:
		fmt.Fprintf(&b, "%s some of these alerts, but not all. ", cover(prs))
	case len(errs) > 0:
		fmt.Fprintf(&b, "Dependabot couldn't open a pull request: %s. ", strings.Join(errs, "; "))
	default:
		b.WriteString("Dependabot hasn't opened a pull request for this. ")
	}
	b.WriteString("Update the package, or dismiss the alerts if they don't apply. I'll close this once pull requests cover every alert or none is open.\n\n")
	b.WriteString(gh.Marker(gh.MarkerSecurity, g.ID))
	return b.String()
}
