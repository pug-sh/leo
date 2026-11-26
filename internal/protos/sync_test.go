package protos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pug-sh/leo/internal/gh/ghtest"
	"github.com/pug-sh/leo/internal/git/gittest"
)

const at = "2026-11-25T00:00:00Z"

// syncFixture returns a checkout of an SDK's base commit and the diff a codegen job made on it:
// a new pin, a changed, an added and a removed proto, and changed generated code. It also returns
// the tree of proto/ once the diff applies.
func syncFixture(t *testing.T) (SyncInput, string) {
	root := t.TempDir()
	r := gittest.Init(t, filepath.Join(root, "sdk-a"))
	r.WriteFile("Makefile", "PROTO_COMMIT := 8b5d0680b10c4693b4d9d15d3dbfc663\n")
	r.WriteFile("proto/sdk/v1/a.proto", "old\n")
	r.WriteFile("proto/sdk/v1/gone.proto", "gone\n")
	r.WriteFile("gen/a.ts", "old\n")
	base := r.Commit(at, "Start")

	r.WriteFile("Makefile", "PROTO_COMMIT := 432c927edc274179ad23f1498a6ad20d\n")
	r.WriteFile("proto/sdk/v1/a.proto", "new\n")
	r.WriteFile("proto/sdk/v1/b.proto", "added\n")
	r.WriteFile("gen/a.ts", "new\n")
	os.Remove(filepath.Join(r.Dir, "proto/sdk/v1/gone.proto"))
	r.Git(at, "add", "-A")
	protoTree := r.Git(at, "write-tree", "--prefix=proto/")
	patch := filepath.Join(root, "changes.patch")
	if err := os.WriteFile(patch, []byte(r.Git(at, "diff", "--cached", "--binary")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git(at, "reset", "--quiet", "--hard")
	return SyncInput{Repo: "sdk-a", Branch: "main", Base: base, Dir: r.Dir, Patch: patch, BSR: bsr, Codegen: "success"}, protoTree
}

func TestSyncOpensPullRequest(t *testing.T) {
	in, _ := syncFixture(t)
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/sdk-a/pulls", 200, `[]`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/ref/heads/leo/sync-protos", 404, `{"message": "Not Found"}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/commits/"+in.Base, 200, `{"sha": "base", "tree": {"sha": "basetree"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/issues", 200, `[]`)
	c, out := s.Client(true)
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"dry-run: sdk-a: write 5 changed files on top of tree basetree",
		fmt.Sprintf(`dry-run: sdk-a: commit "Sync protos to 432c927e" on top of %s`, in.Base[:8]),
		"dry-run: sdk-a: create branch leo/sync-protos at the new commit",
		`dry-run: sdk-a: open pull request "Sync protos to 432c927e" from leo/sync-protos`,
		"    | Syncs `proto/` to `buf.build/pugsh/pug:432c927edc274179ad23f1498a6ad20d`, from pug's [commit d4c2ea2c](https://github.com/pug-sh/pug/commit/d4c2ea2c8c95c31c9253765889bdf43ef34f9da9)",
		"    | - `proto/sdk/v1/a.proto`\n    | - `proto/sdk/v1/b.proto`\n    | - `proto/sdk/v1/gone.proto` (removed)\n",
		"    | With them, 2 other files changed: the Makefile's pin and the generated code.",
		"    | <!-- leo:protos 432c927edc274179ad23f1498a6ad20d -->",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
}

func TestSyncLeavesSomeoneElsesBranch(t *testing.T) {
	in, _ := syncFixture(t)
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/sdk-a/pulls", 200, `[{"number": 5, "title": "Sync protos to 8b5d0680"}]`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/ref/heads/leo/sync-protos", 200, `{"object": {"sha": "tip"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/compare/main...leo/sync-protos", 200, `{"commits": [
	  {"sha": "c1", "author": {"login": "leo-the-pug[bot]"}}, {"sha": "c2", "author": {"login": "poluruprvn"}}]}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/issues/5/comments", 200, `[
	  {"user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:protos-locked 8b5d0680b10c4693b4d9d15d3dbfc663 -->"}]`)
	c, out := s.Client(true)
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	want := "dry-run: sdk-a#5: comment \"pug's protos moved to `432c927e` with its [commit d4c2ea2c](https://github.com/pug-sh/pug/commit/d4c2ea2c8c95c31c9253765889bdf43ef34f9da9), but this branch has commits from someone else, so I've left it alone."
	if !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if strings.Contains(out.String(), "write ") {
		t.Errorf("wrote to someone else's branch:\n%s", out)
	}

	// The comment goes once per pug commit.
	s.REST("GET", "/repos/pug-sh/sdk-a/issues/5/comments", 200, `[
	  {"user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:protos-locked 432c927edc274179ad23f1498a6ad20d -->"}]`)
	c, out = s.Client(true)
	in, _ = syncFixture(t)
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "comment") {
		t.Errorf("commented twice:\n%s", out)
	}
}

func TestSyncReplacesBranchPastWorkflowChanges(t *testing.T) {
	in, _ := syncFixture(t)
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/sdk-a/pulls", 200, `[{"number": 5, "title": "Sync protos to 8b5d0680"}]`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/ref/heads/leo/sync-protos", 200, `{"object": {"sha": "tip"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/compare/main...leo/sync-protos", 200, `{"commits": [{"sha": "c1", "author": {"login": "leo-the-pug[bot]"}}]}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/commits/"+in.Base, 200, `{"sha": "base", "tree": {"sha": "basetree"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/commits/tip", 200, `{"sha": "tip", "tree": {"sha": "oldtree"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/trees/oldtree", 200, `{"sha": "oldtree", "tree": [
	  {"path": "Makefile", "type": "blob", "sha": "m"}, {"path": "proto", "type": "tree", "sha": "oldprotos"}]}`)
	s.REST("POST", "/repos/pug-sh/sdk-a/git/blobs", 201, `{"sha": "blob"}`)
	s.REST("POST", "/repos/pug-sh/sdk-a/git/trees", 201, `{"sha": "newtree"}`)
	s.REST("POST", "/repos/pug-sh/sdk-a/git/commits", 201, `{"sha": "0123456789abcdef"}`)
	s.REST("PATCH", "/repos/pug-sh/sdk-a/git/refs/heads/leo/sync-protos", 422,
		"{\"message\": \"refusing to allow a GitHub App to create or update workflow `.github/workflows/ci.yml` without `workflows` permission\"}")
	s.REST("DELETE", "/repos/pug-sh/sdk-a/git/refs/heads/leo/sync-protos", 204, ``)
	s.REST("POST", "/repos/pug-sh/sdk-a/git/refs", 201, `{"ref": "refs/heads/leo/sync-protos"}`)
	s.REST("POST", "/repos/pug-sh/sdk-a/pulls", 201, `{"number": 6, "html_url": "https://github.com/pug-sh/sdk-a/pull/6"}`)
	s.REST("POST", "/repos/pug-sh/sdk-a/issues/5/comments", 201, `{}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/issues", 200, `[]`)
	c, out := s.Client(false)
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := strings.Join([]string{
		"sdk-a: write 5 changed files on top of tree basetree",
		`sdk-a: commit "Sync protos to 432c927e" on top of ` + in.Base[:8],
		"sdk-a: force leo/sync-protos to 01234567",
		"sdk-a: main changed .github/workflows/ since leo/sync-protos's base, so I'm replacing the branch",
		"sdk-a: delete branch leo/sync-protos",
		"sdk-a: create branch leo/sync-protos at 01234567",
		`sdk-a: open pull request "Sync protos to 432c927e" from leo/sync-protos`,
		"https://github.com/pug-sh/sdk-a/pull/6",
		`sdk-a#5: comment "Replaced by #6: main changed .github/workflows/ since this branch's base, and I can't move a branch past workflow changes."`,
	}, "\n")
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestSyncLeavesABranchThatHasTheseProtos(t *testing.T) {
	in, protoTree := syncFixture(t)
	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/sdk-a/pulls", 200, `[{"number": 5, "title": "Sync protos to 8b5d0680"}]`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/ref/heads/leo/sync-protos", 200, `{"object": {"sha": "tip"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/compare/main...leo/sync-protos", 200, `{"commits": [{"sha": "c1", "author": {"login": "leo-the-pug[bot]"}}]}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/commits/tip", 200, `{"sha": "tip", "tree": {"sha": "tiptree"}}`)
	s.REST("GET", "/repos/pug-sh/sdk-a/git/trees/tiptree", 200, fmt.Sprintf(`{"sha": "tiptree", "tree": [
	  {"path": "Makefile", "type": "blob", "sha": "m"}, {"path": "proto", "type": "tree", "sha": %q}]}`, protoTree))
	s.REST("GET", "/repos/pug-sh/sdk-a/issues", 200, `[]`)
	c, out := s.Client(false)
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out.String()), "sdk-a: leo/sync-protos already has these protos"; got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestSyncReportsCodegenFailure(t *testing.T) {
	var log strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&log, "line %d\n", i)
	}
	log.WriteString("error: ```make protos``` failed\n")
	path := filepath.Join(t.TempDir(), "codegen.log")
	os.WriteFile(path, []byte(log.String()), 0o644)

	s := ghtest.New(t)
	s.REST("GET", "/repos/pug-sh/sdk-a/issues", 200, `[]`)
	c, out := s.Client(true)
	in := SyncInput{Repo: "sdk-a", Log: path, BSR: bsr, Codegen: "failure", RunURL: "https://github.com/pug-sh/leo/actions/runs/1"}
	if err := Sync(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`dry-run: sdk-a: open issue "Proto sync failed"`,
		"    | Syncing protos to `432c927e` failed: the codegen job failed.",
		"[The run](https://github.com/pug-sh/leo/actions/runs/1) has the whole log.",
		"    | ````text\n    | line 52\n",
		"    | error: ```make protos``` failed\n    | ````\n",
		"    | <!-- leo:protos-failed 432c927edc274179ad23f1498a6ad20d -->",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "line 51\n") {
		t.Error("the log isn't cut to its end")
	}
}

func TestReportFailureRespectsAPersonsClose(t *testing.T) {
	closed := func(commit string) string {
		return fmt.Sprintf(`[{"number": 9, "state": "closed", "closed_at": "2026-11-25T00:00:00Z", "title": "Proto sync failed",
		  "user": {"login": "leo-the-pug[bot]"}, "body": "<!-- leo:protos-failed %s -->"}]`, commit)
	}
	for _, tt := range []struct {
		name   string
		issues string
		opens  bool
	}{
		{"same pug commit", closed(bsr.Commit), false},
		{"newer pug commit", closed("8b5d0680b10c4693b4d9d15d3dbfc663"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := ghtest.New(t)
			s.REST("GET", "/repos/pug-sh/sdk-a/issues", 200, tt.issues)
			s.REST("GET", "/repos/pug-sh/sdk-a/issues/9", 200, `{"number": 9, "state": "closed", "closed_by": {"login": "poluruprvn"}}`)
			c, out := s.Client(true)
			if err := ReportFailure(context.Background(), c, Failure{Repo: "sdk-a", BSR: bsr, What: "the codegen job failed"}); err != nil {
				t.Fatal(err)
			}
			if opened := strings.Contains(out.String(), "open issue"); opened != tt.opens {
				t.Errorf("opened = %v, want %v:\n%s", opened, tt.opens, out)
			}
		})
	}
}

func TestApplyPatchRefusesGitHubChanges(t *testing.T) {
	root := t.TempDir()
	r := gittest.Init(t, filepath.Join(root, "sdk"))
	r.WriteFile("Makefile", "x\n")
	r.Commit(at, "Start")
	r.WriteFile(".github/workflows/ci.yml", "on: push\n")
	r.Git(at, "add", "-A")
	patch := filepath.Join(root, "changes.patch")
	os.WriteFile(patch, []byte(r.Git(at, "diff", "--cached")+"\n"), 0o644)
	r.Git(at, "reset", "--quiet", "--hard")
	os.RemoveAll(filepath.Join(r.Dir, ".github"))
	if _, err := applyPatch(context.Background(), r.Dir, patch); err == nil || !strings.Contains(err.Error(), ".github/workflows/ci.yml") {
		t.Errorf("applyPatch = %v", err)
	}
}

func TestTail(t *testing.T) {
	if got := Tail("a\nb\nc\nd\n", 2, 100); got != "c\nd" {
		t.Errorf("Tail by lines = %q", got)
	}
	if got := Tail("aaaa\nbbbb\ncccc", 10, 7); got != "cccc" {
		t.Errorf("Tail by bytes = %q", got)
	}
}
