package protos

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pug-sh/leo/internal/config"
	"github.com/pug-sh/leo/internal/gh/ghtest"
	"github.com/pug-sh/leo/internal/git/gittest"
)

var bsr = BSRCommit{Commit: "432c927edc274179ad23f1498a6ad20d", SourceControlURL: "https://github.com/pug-sh/pug/commit/d4c2ea2c8c95c31c9253765889bdf43ef34f9da9"}

const makefile = "PROTO_MODULE := buf.build/pugsh/pug\nPROTO_COMMIT := 8b5d0680b10c4693b4d9d15d3dbfc663\n" +
	"PROTO_PATHS := --path sdk\n\nsync-protos:\n\tbuf export $(PROTO_MODULE):$(PROTO_COMMIT) --output proto $(PROTO_PATHS)\n\nprotos:\n\tbuf generate\n"

func TestDetectToolchain(t *testing.T) {
	tests := []struct {
		root []string
		want string
	}{
		{[]string{"Makefile", "Package.swift", "Package.resolved"}, "swift"},
		{[]string{"Makefile", "gradlew", "build.gradle.kts"}, "gradle"},
		{[]string{"Makefile", "pubspec.yaml"}, "flutter"},
		{[]string{"Makefile", "go.mod", "go.sum"}, "go"},
		{[]string{"Makefile", "Gemfile", "pug-sh.gemspec"}, "ruby"},
		{[]string{"Makefile", "bun.lock", "package.json"}, "bun"},
		{[]string{"Makefile", "Cargo.toml"}, ""},
	}
	for _, tt := range tests {
		if got := DetectToolchain(tt.root); got != tt.want {
			t.Errorf("DetectToolchain(%v) = %q, want %q", tt.root, got, tt.want)
		}
	}
}

func TestDiffTrees(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write := func(dir, name, content string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	write(a, "sdk/v1/same.proto", "same")
	write(b, "sdk/v1/same.proto", "same")
	write(a, "sdk/v1/changed.proto", "new")
	write(b, "sdk/v1/changed.proto", "old")
	write(a, "sdk/v1/added.proto", "new")
	write(b, "sdk/v1/removed.proto", "old")
	if n, err := diffTrees(a, b); err != nil || n != 3 {
		t.Errorf("diffTrees = %d, %v; want 3", n, err)
	}
	if n, err := diffTrees(a, filepath.Join(b, "missing")); err != nil || n != 3 {
		t.Errorf("diffTrees against a missing proto/ = %d, %v; want 3", n, err)
	}
}

func TestPlan(t *testing.T) {
	root := t.TempDir()
	drifted := gittest.Init(t, filepath.Join(root, "sdk-a"))
	drifted.WriteFile("Makefile", makefile)
	drifted.WriteFile("bun.lock", "{}\n")
	drifted.WriteFile("proto/sdk/v1/a.proto", "old\n")
	drifted.Commit("2026-11-01T00:00:00Z", "Start")
	inSync := gittest.Init(t, filepath.Join(root, "sdk-b"))
	inSync.WriteFile("Makefile", makefile)
	inSync.WriteFile("go.mod", "module x\n")
	inSync.WriteFile("proto/sdk/v1/a.proto", "new\n")
	inSync.Commit("2026-11-01T00:00:00Z", "Start")
	broken := gittest.Init(t, filepath.Join(root, "sdk-c"))
	broken.WriteFile("Makefile", "build:\n\tgo build ./...\n")
	broken.WriteFile("go.mod", "module x\n")
	broken.Commit("2026-11-01T00:00:00Z", "Start")

	s := ghtest.New(t)
	s.REST("GET", "/orgs/pug-sh/repos", 200, `[
	  {"name": "app", "visibility": "public", "default_branch": "main"},
	  {"name": "sdk-a", "visibility": "public", "default_branch": "main", "topics": ["pug-sdk"]},
	  {"name": "sdk-b", "visibility": "public", "default_branch": "main", "topics": ["pug-sdk"]},
	  {"name": "sdk-c", "visibility": "public", "default_branch": "main", "topics": ["pug-sdk"]}
	]`)
	s.REST("GET", "/repos/pug-sh/sdk-b/issues", 200, `[{"number": 3, "state": "open", "title": "Proto sync failed",
	  "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:protos-failed 8b5d0680b10c4693b4d9d15d3dbfc663 -->"}]`)
	s.REST("GET", "/repos/pug-sh/sdk-c/issues", 200, `[]`)

	c, out := s.Client(true)
	cfg := &config.Config{Repos: map[string]config.Repo{"sdk-a": {Codegen: config.Codegen{Bun: "1.4.4", Node: "24"}}}}
	p := NewPlanner(c, cfg, "buf")
	p.cloneURL = func(repo string) string { return "file://" + filepath.Join(root, repo) }
	p.resolve = func(context.Context) (BSRCommit, error) { return bsr, nil }
	var exported [][]string
	p.export = func(_ context.Context, commit string, paths []string, dir string) error {
		if commit != bsr.Commit {
			t.Errorf("exported %s", commit)
		}
		exported = append(exported, paths)
		os.MkdirAll(filepath.Join(dir, "sdk/v1"), 0o755)
		return os.WriteFile(filepath.Join(dir, "sdk/v1/a.proto"), []byte("new\n"), 0o644)
	}

	got, targets, err := p.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != bsr {
		t.Errorf("BSR commit = %+v", got)
	}
	want := []Target{{Repo: "sdk-a", Branch: "main", Toolchain: "bun", Target: "protos", Bun: "1.4.4", Node: "24"}}
	if !slices.Equal(targets, want) {
		t.Errorf("targets = %+v, want %+v", targets, want)
	}
	if len(exported) != 2 || !slices.Equal(exported[0], []string{"sdk"}) {
		t.Errorf("exported %v", exported)
	}
	for _, line := range []string{
		"sdk-a: 1 file differs in proto/ from 432c927e",
		"dry-run: sdk-b#3: comment \"proto/ matches `432c927e` now, so I'm closing this.\"",
		"dry-run: sdk-b#3: close",
		"warning: sdk-c: Makefile: no PROTO_COMMIT line",
		`dry-run: sdk-c: open issue "Proto sync failed"`,
		"    | Syncing protos to `432c927e` failed: I couldn't check its protos. The protos come from pug's [commit d4c2ea2c]",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out.String(), "app") {
		t.Errorf("checked a repo without the pug-sdk topic:\n%s", out)
	}
}
