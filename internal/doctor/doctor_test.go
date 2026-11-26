package doctor

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pug-sh/leo/internal/gh/ghtest"
)

func TestEcosystems(t *testing.T) {
	tests := []struct {
		root      []string
		workflows bool
		want      []string
	}{
		{[]string{"go.mod", "go.sum", "Dockerfile"}, true, []string{"gomod", "docker", "github-actions"}},
		{[]string{"package.json", "bun.lock"}, true, []string{"bun", "github-actions"}},
		{[]string{"package.json", "package-lock.json"}, false, []string{"npm"}},
		{[]string{"Gemfile", "pug-sh.gemspec"}, true, []string{"bundler", "github-actions"}},
		{[]string{"pubspec.yaml"}, false, []string{"pub"}},
		{[]string{"gradlew", "settings.gradle.kts"}, false, []string{"gradle"}},
		{[]string{"Package.swift", "Package.resolved"}, false, []string{"swift"}},
		{[]string{"pyproject.toml", "uv.lock"}, false, []string{"uv"}},
		{[]string{"requirements.txt"}, false, []string{"pip"}},
		{[]string{"README.md", "favicon.ico"}, false, nil},
	}
	for _, tt := range tests {
		if got := Ecosystems(tt.root, tt.workflows); !slices.Equal(got, tt.want) {
			t.Errorf("Ecosystems(%v, %v) = %v, want %v", tt.root, tt.workflows, got, tt.want)
		}
	}
}

func TestDependabotProblems(t *testing.T) {
	config := []byte(`version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "monthly"
    groups:
      protobuf:
        patterns: ["google.golang.org/protobuf"]
        update-types: ["major"]
      gomod-minor-patch:
        patterns: ["*"]
        update-types: ["minor", "patch"]
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
  - package-ecosystem: "docker"
    directories: ["/", "/infra/dev"]
    schedule:
      interval: "Monthly"
    groups:
      docker:
        patterns: ["*"]
`)
	got := DependabotProblems(config, []string{"gomod", "docker", "github-actions", "bun"})
	want := []string{
		"has no Dependabot group for `docker`'s minor and patch updates",
		"has Dependabot check `github-actions` weekly, not monthly",
		"has no Dependabot group for `github-actions`'s minor and patch updates",
		"has Dependabot leave `bun` dependencies alone",
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := DependabotProblems([]byte("updates: ["), []string{"gomod"}); len(got) != 1 || !strings.Contains(got[0], "doesn't parse") {
		t.Errorf("broken config: %v", got)
	}
}

const goodDependabot = `version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "monthly"
    groups:
      gomod-minor-patch:
        patterns: ["*"]
        update-types: ["minor", "patch"]
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "monthly"
    groups:
      actions-minor-patch:
        patterns: ["*"]
        update-types: ["minor", "patch"]
`

func newServer(t *testing.T) *ghtest.Server {
	s := ghtest.New(t)
	s.REST("GET", "/orgs/pug-sh/repos", 200, `[
	  {"name": "favicons", "visibility": "public"},
	  {"name": "leo", "visibility": "public"},
	  {"name": "sdk-x", "visibility": "public"}
	]`)

	s.REST("GET", "/repos/pug-sh/favicons/contents/", 200, `[{"name": "README.md", "type": "file"}, {"name": "icons", "type": "dir"}]`)
	s.REST("GET", "/repos/pug-sh/favicons/actions/workflows", 200, `{"workflows": [
	  {"name": "Dependabot Updates", "path": "dynamic/dependabot/dependabot-updates", "state": "active"}]}`)

	s.REST("GET", "/repos/pug-sh/leo/contents/", 200, `[{"name": "go.mod"}, {"name": "go.sum"}, {"name": ".github"}]`)
	s.REST("GET", "/repos/pug-sh/leo/actions/workflows", 200, `{"workflows": [
	  {"name": "CI", "path": ".github/workflows/ci.yml", "state": "active"},
	  {"name": "OSV-Scanner", "path": ".github/workflows/osv-scanner.yml", "state": "active"}]}`)
	s.REST("GET", "/repos/pug-sh/leo/contents/.github/dependabot.yml", 200,
		fmt.Sprintf(`{"type": "file", "encoding": "base64", "content": %q}`, base64.StdEncoding.EncodeToString([]byte(goodDependabot))))
	s.REST("GET", "/repos/pug-sh/leo/dependabot/alerts", 200, `[]`)
	s.REST("GET", "/repos/pug-sh/leo/automated-security-fixes", 200, `{"enabled": true, "paused": false}`)

	s.REST("GET", "/repos/pug-sh/sdk-x/contents/", 200, `[{"name": "package.json"}, {"name": "bun.lock"}, {"name": "Makefile"}]`)
	s.REST("GET", "/repos/pug-sh/sdk-x/actions/workflows", 200, `{"workflows": [
	  {"name": "CI", "path": ".github/workflows/ci.yml", "state": "active"},
	  {"name": "OSV-Scanner", "path": ".github/workflows/osv-scanner.yml", "state": "disabled_inactivity"}]}`)
	s.REST("GET", "/repos/pug-sh/sdk-x/contents/.github/dependabot.yml", 404, `{"message": "Not Found"}`)
	s.REST("GET", "/repos/pug-sh/sdk-x/contents/.github/dependabot.yaml", 404, `{"message": "Not Found"}`)
	s.REST("GET", "/repos/pug-sh/sdk-x/dependabot/alerts", 403, `{"message": "Dependabot alerts are disabled for this repository."}`)
	s.REST("GET", "/repos/pug-sh/sdk-x/automated-security-fixes", 403, `{"message": "Resource not accessible by integration"}`)
	return s
}

func TestRunDryRun(t *testing.T) {
	s := newServer(t)
	s.REST("GET", "/repos/pug-sh/leo/issues", 200, `[]`)
	c, out := s.Client(true)
	if err := Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"sdk-x: has no `pug-sdk` topic, so Leo doesn't sync its protos",
		"sdk-x: has no `.github/dependabot.yml`",
		"sdk-x: has its OSV-Scanner workflow disabled inactivity",
		"sdk-x: has Dependabot alerts off",
		"warning: can't see whether Dependabot security updates are on",
		`dry-run: leo: open issue "Repo hygiene"`,
		"    | **sdk-x**\n    | \n    | - has no `pug-sdk` topic",
		"    | <!-- leo:doctor -->",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"favicons", "leo: has", "**leo**"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("output has %q:\n%s", unwanted, out)
		}
	}
}

func TestReportClosesTheIssueOnceAllIsWell(t *testing.T) {
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/leo/issues", 200, `[{"number": 3, "state": "open", "title": "Repo hygiene",
	  "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:doctor -->"}]`)
	c, out := s.Client(true)
	if err := report(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	if want := "dry-run: leo#3: close"; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestReportRespectsAPersonsClose(t *testing.T) {
	findings := []Finding{{Repo: "sdk-x", Problem: "has no `.github/dependabot.yml`"}}
	body := Body(findings)
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/leo/issues", 200, fmt.Sprintf(`[{"number": 3, "state": "closed", "title": "Repo hygiene",
	  "user": {"login": "leo-the-pug[bot]"}, "body": %q, "closed_at": "2026-11-26T00:00:00Z"}]`, body))
	s.REST("GET", "/repos/pug-sh/leo/issues/3", 200, `{"number": 3, "state": "closed", "closed_by": {"login": "poluruprvn"}}`)
	c, out := s.Client(true)
	if err := report(context.Background(), c, findings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "open issue") {
		t.Errorf("reopened what a person closed:\n%s", out)
	}
	if err := report(context.Background(), c, append(findings, Finding{Repo: "sdk-y", Problem: "has Dependabot alerts off"})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "open issue") {
		t.Errorf("didn't open an issue for a new finding:\n%s", out)
	}
}
