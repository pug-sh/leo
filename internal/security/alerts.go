package security

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pug-sh/leo/internal/gh"
)

const (
	sourceDependabot = "Dependabot"
	sourceOSV        = "OSV-Scanner"
	osvTool          = "osv-scanner"
)

type Alert struct {
	Source string
	Number int
	// IDs are the advisory's IDs: its GHSA and CVE, or OSV-Scanner's ID and its aliases.
	IDs         []string
	Package     string
	Ecosystem   string
	Manifest    string
	Severity    string
	Fixed       string
	URL         string
	Created     time.Time
	PR          int
	UpdateError string
}

func (a Alert) dir() string { return path.Dir(a.Manifest) }

func fromDependabot(a gh.DependabotAlert, u gh.DependabotUpdate) Alert {
	alert := Alert{
		Source:      sourceDependabot,
		Number:      a.Number,
		IDs:         nonEmpty(a.GHSA, a.CVE),
		Package:     a.Package,
		Ecosystem:   a.Ecosystem,
		Manifest:    a.Manifest,
		Severity:    a.Severity,
		Fixed:       a.Fixed,
		URL:         a.URL,
		Created:     a.Created,
		UpdateError: u.Error,
	}
	if u.OpenPR() {
		alert.PR = u.PR
	}
	return alert
}

func fromOSV(a gh.CodeScanningAlert, aliases []string) Alert {
	pkg, aka := parseOSVMessage(a.Message)
	return Alert{
		Source:   sourceOSV,
		Number:   a.Number,
		IDs:      nonEmpty(append([]string{a.Rule}, append(aka, aliases...)...)...),
		Package:  pkg,
		Manifest: a.Path,
		Severity: a.Severity,
		URL:      a.URL,
		Created:  a.Created,
	}
}

var (
	osvPackageRE = regexp.MustCompile(`^Package '([^']+)' is vulnerable`)
	osvAliasesRE = regexp.MustCompile(`\(also known as '(.*)'\)`)
)

// parseOSVMessage reads the package and aliases out of OSV-Scanner's result message, which is
// "Package 'name@version' is vulnerable to 'ID' (also known as 'A', 'B').".
func parseOSVMessage(msg string) (pkg string, aliases []string) {
	if m := osvPackageRE.FindStringSubmatch(msg); m != nil {
		pkg = m[1]
		if i := strings.LastIndex(pkg, "@"); i > 0 {
			pkg = pkg[:i]
		}
	}
	if m := osvAliasesRE.FindStringSubmatch(msg); m != nil {
		aliases = strings.Split(m[1], "', '")
	}
	return pkg, aliases
}

func nonEmpty(ids ...string) []string {
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Group is the open alerts for one advisory, however many IDs it goes by.
type Group struct {
	ID     string
	IDs    []string
	Alerts []Alert
}

// Has reports whether id is one of the group's IDs.
func (g Group) Has(id string) bool {
	return slices.ContainsFunc(g.IDs, func(x string) bool { return strings.EqualFold(x, id) })
}

func (g Group) oldest() time.Time {
	return slices.MinFunc(g.Alerts, func(a, b Alert) int { return a.Created.Compare(b.Created) }).Created
}

func (g Group) newest() time.Time {
	return slices.MaxFunc(g.Alerts, func(a, b Alert) int { return a.Created.Compare(b.Created) }).Created
}

// GroupAlerts puts alerts that share any ID into one group, so a vulnerability that Dependabot
// and OSV-Scanner both report makes one group. A group goes by its GHSA ID when it has one.
func GroupAlerts(alerts []Alert) []Group {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	key := strings.ToUpper
	for _, a := range alerts {
		for i := 1; i < len(a.IDs); i++ {
			if ra, rb := find(key(a.IDs[0])), find(key(a.IDs[i])); ra != rb {
				parent[rb] = ra
			}
		}
	}

	byRoot := map[string]*Group{}
	var roots []string
	for _, a := range alerts {
		if len(a.IDs) == 0 {
			continue
		}
		root := find(key(a.IDs[0]))
		g, ok := byRoot[root]
		if !ok {
			g = &Group{}
			byRoot[root] = g
			roots = append(roots, root)
		}
		g.Alerts = append(g.Alerts, a)
		for _, id := range a.IDs {
			if !g.Has(id) {
				g.IDs = append(g.IDs, id)
			}
		}
	}

	groups := make([]Group, 0, len(roots))
	for _, root := range roots {
		g := byRoot[root]
		slices.Sort(g.IDs)
		g.ID = g.IDs[0]
		if i := slices.IndexFunc(g.IDs, func(id string) bool { return strings.HasPrefix(id, "GHSA-") }); i >= 0 {
			g.ID = g.IDs[i]
		}
		slices.SortFunc(g.Alerts, func(a, b Alert) int {
			return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Number, b.Number))
		})
		groups = append(groups, *g)
	}
	slices.SortFunc(groups, func(a, b Group) int { return cmp.Compare(a.ID, b.ID) })
	return groups
}

// Coverage returns the open pull requests that fix the group's alerts, and whether they cover
// every one. No pull request is ever linked to an OSV-Scanner alert, so it counts as covered when
// a Dependabot alert for the same package in the same directory has one.
func Coverage(g Group, all []Alert) (prs []int, covered bool) {
	covered = true
	for _, a := range g.Alerts {
		pr := a.PR
		if pr == 0 && a.Source == sourceOSV && a.Package != "" {
			for _, d := range all {
				if d.Source == sourceDependabot && d.PR != 0 && strings.EqualFold(d.Package, a.Package) && d.dir() == a.dir() {
					pr = d.PR
					break
				}
			}
		}
		if pr == 0 {
			covered = false
			continue
		}
		if !slices.Contains(prs, pr) {
			prs = append(prs, pr)
		}
	}
	slices.Sort(prs)
	return prs, covered
}

var severityRank = map[string]int{"low": 1, "medium": 2, "moderate": 2, "high": 3, "critical": 4}

func (g Group) severity() string {
	worst := ""
	for _, a := range g.Alerts {
		if s := strings.ToLower(a.Severity); severityRank[s] > severityRank[worst] {
			worst = s
		}
	}
	return worst
}
