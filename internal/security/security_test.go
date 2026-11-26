package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pug-sh/leo/internal/gh/ghtest"
)

var now = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func osvServer(t *testing.T) *OSV {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/vulns/GO-2026-0001":
			w.Write([]byte(`{"id": "GO-2026-0001", "aliases": ["CVE-2026-1111", "GHSA-cccc-cccc-cccc"]}`))
		case "/v1/vulns/GHSA-aaaa-aaaa-aaaa":
			w.Write([]byte(`{"id": "GHSA-aaaa-aaaa-aaaa", "aliases": ["CVE-2026-0001"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewOSV(srv.URL)
}

func TestRunDryRun(t *testing.T) {
	s := ghtest.New(t)
	s.REST("GET", "/orgs/pug-sh/repos", 200, `[{"name": "app", "visibility": "public"}, {"name": "favicons", "visibility": "public"}]`)

	s.REST("GET", "/repos/pug-sh/app/dependabot/alerts", 200, `[
	  {"number": 1, "state": "open", "html_url": "https://github.com/pug-sh/app/security/dependabot/1", "created_at": "2026-11-26T09:00:00Z",
	   "dependency": {"package": {"ecosystem": "npm", "name": "lodash"}, "manifest_path": "package.json"},
	   "security_advisory": {"ghsa_id": "GHSA-aaaa-aaaa-aaaa", "cve_id": "CVE-2026-0001", "severity": "high"},
	   "security_vulnerability": {"first_patched_version": {"identifier": "4.17.21"}}},
	  {"number": 2, "state": "open", "html_url": "https://github.com/pug-sh/app/security/dependabot/2", "created_at": "2026-11-26T10:00:00Z",
	   "dependency": {"package": {"ecosystem": "rubygems", "name": "rack"}, "manifest_path": "Gemfile.lock"},
	   "security_advisory": {"ghsa_id": "GHSA-bbbb-bbbb-bbbb", "severity": "critical"},
	   "security_vulnerability": {"first_patched_version": {"identifier": "3.1.2"}}},
	  {"number": 3, "state": "open", "html_url": "https://github.com/pug-sh/app/security/dependabot/3", "created_at": "2026-11-26T02:00:00Z",
	   "dependency": {"package": {"ecosystem": "npm", "name": "vite"}, "manifest_path": "package.json"},
	   "security_advisory": {"ghsa_id": "GHSA-eeee-eeee-eeee", "severity": "low"}}
	]`)
	s.GraphQL("vulnerabilityAlerts(", map[string]string{"name": "app"}, `{"repository": {"vulnerabilityAlerts": {"nodes": [
	  {"number": 1, "dependabotUpdate": {"pullRequest": {"number": 10, "state": "OPEN"}}},
	  {"number": 2, "dependabotUpdate": {"pullRequest": null, "error": {"title": "Dependabot can't resolve your Ruby dependency files"}}},
	  {"number": 3, "dependabotUpdate": null}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.REST("GET", "/repos/pug-sh/app/code-scanning/alerts", 200, `[
	  {"number": 5, "html_url": "https://github.com/pug-sh/app/security/code-scanning/5", "created_at": "2026-11-26T09:30:00Z",
	   "rule": {"id": "GHSA-aaaa-aaaa-aaaa", "security_severity_level": "high"},
	   "most_recent_instance": {"location": {"path": "bun.lock"}, "message": {"text": "Package 'lodash@4.17.20' is vulnerable to 'GHSA-aaaa-aaaa-aaaa' (also known as 'CVE-2026-0001')."}}},
	  {"number": 6, "html_url": "https://github.com/pug-sh/app/security/code-scanning/6", "created_at": "2026-11-26T11:30:00Z",
	   "rule": {"id": "GO-2026-0001", "security_severity_level": "medium"},
	   "most_recent_instance": {"location": {"path": "go.mod"}, "message": {"text": "Package 'golang.org/x/net@v0.30.0' is vulnerable to 'GO-2026-0001'."}}}
	]`)
	s.REST("GET", "/repos/pug-sh/app/issues", 200, `[
	  {"number": 24, "state": "open", "title": "Security: GHSA-ffff-ffff-ffff in x", "user": {"login": "someone"},
	   "body": "<!-- leo:security GHSA-ffff-ffff-ffff -->"},
	  {"number": 23, "state": "closed", "closed_at": "2026-11-26T07:00:00Z", "title": "Security: GHSA-eeee-eeee-eeee in vite",
	   "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:security GHSA-eeee-eeee-eeee -->"},
	  {"number": 22, "state": "closed", "closed_at": "2026-11-26T07:00:00Z", "title": "Security: GHSA-bbbb-bbbb-bbbb in rack",
	   "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:security GHSA-bbbb-bbbb-bbbb -->"},
	  {"number": 21, "state": "open", "title": "Security: GHSA-dddd-dddd-dddd in left-pad",
	   "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:security GHSA-dddd-dddd-dddd -->"},
	  {"number": 20, "state": "open", "title": "Security: GHSA-aaaa-aaaa-aaaa in lodash",
	   "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:security GHSA-aaaa-aaaa-aaaa -->"},
	  {"number": 19, "state": "open", "title": "Bump", "user": {"login": "leo-the-pug[bot]"},
	   "body": "<!-- leo:security GHSA-aaaa-aaaa-aaaa -->", "pull_request": {"url": "https://api.github.com/repos/pug-sh/app/pulls/19"}}
	]`)
	s.REST("GET", "/repos/pug-sh/app/issues/22", 200, `{"number": 22, "state": "closed", "closed_by": {"login": "poluruprvn"}}`)
	s.REST("GET", "/repos/pug-sh/app/issues/23", 200, `{"number": 23, "state": "closed", "closed_by": {"login": "poluruprvn"}}`)

	s.REST("GET", "/repos/pug-sh/favicons/dependabot/alerts", 403, `{"message": "Dependabot alerts are disabled for this repository."}`)
	s.REST("GET", "/repos/pug-sh/favicons/code-scanning/alerts", 404, `{"message": "no analysis found"}`)
	s.REST("GET", "/repos/pug-sh/favicons/issues", 200, `[]`)

	c, out := s.Client(true)
	if err := Run(context.Background(), c, osvServer(t), now); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	for _, want := range []string{
		`dry-run: app#20: comment "#10 covers every alert for GHSA-aaaa-aaaa-aaaa, so I'm closing this."`,
		"dry-run: app#20: close",
		`dry-run: app: open issue "Security: GHSA-bbbb-bbbb-bbbb in rack"`,
		"    | - **Severity:** critical",
		"    | - **Fixed in:** 3.1.2",
		"    | - [Dependabot alert #2](https://github.com/pug-sh/app/security/dependabot/2)",
		"    | Dependabot couldn't open a pull request: Dependabot can't resolve your Ruby dependency files.",
		"    | <!-- leo:security GHSA-bbbb-bbbb-bbbb -->",
		`dry-run: app#21: comment "No open alert remains for GHSA-dddd-dddd-dddd, so I'm closing this."`,
		"dry-run: app#21: close",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	for _, unwanted := range []string{
		"GHSA-cccc", // GO-2026-0001 is only half an hour old
		"vite",      // a person closed #23 and no alert came since
		"app#24",    // someone else's issue, marker or not
		"app#19",    // a pull request
		"favicons",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output mentions %q", unwanted)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", got)
	}
}

func TestIssueBodyIsStable(t *testing.T) {
	g := Group{ID: "GHSA-aaaa-aaaa-aaaa", IDs: []string{"CVE-2026-0001", "GHSA-aaaa-aaaa-aaaa"}, Alerts: []Alert{
		{Source: sourceDependabot, Number: 1, Package: "lodash", Manifest: "package.json", Fixed: "4.17.21", URL: "u1", PR: 10},
		{Source: sourceOSV, Number: 5, Package: "lodash", Manifest: "web/bun.lock", URL: "u5"},
	}}
	body := issueBody("app", g, []int{10})
	if body != issueBody("app", g, []int{10}) {
		t.Error("issueBody isn't deterministic")
	}
	for _, want := range []string{
		"- **Manifests:** `package.json`, `web/bun.lock`",
		"- **Also known as:** CVE-2026-0001",
		"- [Dependabot alert #1](u1), fixed by #10",
		"#10 covers some of these alerts, but not all.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if got := cover([]int{3, 4, 5}); got != "#3, #4 and #5 cover" {
		t.Errorf("cover = %q", got)
	}
}
