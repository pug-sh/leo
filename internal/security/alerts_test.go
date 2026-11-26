package security

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseOSVMessage(t *testing.T) {
	tests := []struct {
		msg     string
		pkg     string
		aliases []string
	}{
		{"Package 'golang.org/x/net@v0.30.0' is vulnerable to 'GO-2026-0001' (also known as 'CVE-2026-1111', 'GHSA-cccc-cccc-cccc').",
			"golang.org/x/net", []string{"CVE-2026-1111", "GHSA-cccc-cccc-cccc"}},
		{"Package '@babel/core@7.20.0' is vulnerable to 'GHSA-aaaa-aaaa-aaaa'.", "@babel/core", nil},
		{"Package 'https://github.com/google/osv-scanner@12345678' is vulnerable to 'OSV-2026-1'.", "https://github.com/google/osv-scanner", nil},
		{"Something else entirely.", "", nil},
	}
	for _, tt := range tests {
		pkg, aliases := parseOSVMessage(tt.msg)
		if pkg != tt.pkg || !slices.Equal(aliases, tt.aliases) {
			t.Errorf("parseOSVMessage(%q) = %q, %q; want %q, %q", tt.msg, pkg, aliases, tt.pkg, tt.aliases)
		}
	}
}

func TestGroupAlerts(t *testing.T) {
	alerts := []Alert{
		{Source: sourceOSV, Number: 5, IDs: []string{"GO-2026-0001", "CVE-2026-1111"}},
		{Source: sourceDependabot, Number: 2, IDs: []string{"GHSA-bbbb-bbbb-bbbb", "CVE-2026-2222"}},
		{Source: sourceDependabot, Number: 1, IDs: []string{"GHSA-cccc-cccc-cccc", "CVE-2026-1111"}},
		{Source: sourceOSV, Number: 7, IDs: []string{"PYSEC-2026-7"}},
		{Source: sourceOSV, Number: 6, IDs: []string{"ghsa-bbbb-bbbb-bbbb"}},
	}
	groups := GroupAlerts(alerts)
	var got []string
	for _, g := range groups {
		var members []string
		for _, a := range g.Alerts {
			members = append(members, fmt.Sprintf("%s#%d", a.Source, a.Number))
		}
		got = append(got, g.ID+": "+strings.Join(members, " "))
	}
	want := []string{
		"GHSA-bbbb-bbbb-bbbb: Dependabot#2 OSV-Scanner#6",
		"GHSA-cccc-cccc-cccc: Dependabot#1 OSV-Scanner#5",
		"PYSEC-2026-7: OSV-Scanner#7",
	}
	if !slices.Equal(got, want) {
		t.Errorf("groups:\n%v\nwant:\n%v", got, want)
	}
	if !groups[1].Has("go-2026-0001") || !groups[1].Has("CVE-2026-1111") || groups[1].Has("PYSEC-2026-7") {
		t.Errorf("group IDs: %v", groups[1].IDs)
	}
}

func TestCoverage(t *testing.T) {
	dep := Alert{Source: sourceDependabot, Number: 1, IDs: []string{"GHSA-a"}, Package: "lodash", Manifest: "web/package.json", PR: 10}
	osvSameDir := Alert{Source: sourceOSV, Number: 5, IDs: []string{"GHSA-b"}, Package: "Lodash", Manifest: "web/bun.lock"}
	osvOtherDir := Alert{Source: sourceOSV, Number: 6, IDs: []string{"GHSA-b"}, Package: "lodash", Manifest: "bun.lock"}
	depNoPR := Alert{Source: sourceDependabot, Number: 2, IDs: []string{"GHSA-b"}, Package: "lodash", Manifest: "bun.lock"}
	all := []Alert{dep, osvSameDir, osvOtherDir, depNoPR}

	tests := []struct {
		name    string
		alerts  []Alert
		prs     []int
		covered bool
	}{
		{"Dependabot alert with a PR", []Alert{dep}, []int{10}, true},
		{"OSV alert in the PR's directory", []Alert{osvSameDir}, []int{10}, true},
		{"OSV alert in another directory", []Alert{osvOtherDir}, nil, false},
		{"Dependabot alert without a PR", []Alert{depNoPR}, nil, false},
		{"partly covered", []Alert{dep, depNoPR}, []int{10}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prs, covered := Coverage(Group{Alerts: tt.alerts}, all)
			if !slices.Equal(prs, tt.prs) || covered != tt.covered {
				t.Errorf("Coverage = %v, %v; want %v, %v", prs, covered, tt.prs, tt.covered)
			}
		})
	}
}

func TestGroupTimesAndSeverity(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	g := Group{Alerts: []Alert{
		{Created: t0.Add(2 * time.Hour), Severity: "moderate"},
		{Created: t0, Severity: "HIGH"},
		{Created: t0.Add(time.Hour), Severity: "low"},
	}}
	if !g.oldest().Equal(t0) || !g.newest().Equal(t0.Add(2*time.Hour)) {
		t.Errorf("oldest %v, newest %v", g.oldest(), g.newest())
	}
	if s := g.severity(); s != "high" {
		t.Errorf("severity = %q", s)
	}
}
