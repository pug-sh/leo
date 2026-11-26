// Package gittest builds git repos for tests, with commits at the times they name.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type Repo struct {
	t   testing.TB
	Dir string
}

// Init creates a repo with a main branch in dir.
func Init(t testing.TB, dir string) *Repo {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Repo{t: t, Dir: dir}
	r.Git("2026-01-01T00:00:00Z", "init", "--quiet", "-b", "main")
	return r
}

// Git runs git in the repo as if at the time at, and returns its trimmed output.
func (r *Repo) Git(at string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Pug", "GIT_AUTHOR_EMAIL=pug@example.com",
		"GIT_COMMITTER_NAME=Pug", "GIT_COMMITTER_EMAIL=pug@example.com",
		"GIT_AUTHOR_DATE="+at, "GIT_COMMITTER_DATE="+at,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *Repo) WriteFile(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.Dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// Commit writes at into each of files, commits everything at the time at and returns the commit.
func (r *Repo) Commit(at, message string, files ...string) string {
	r.t.Helper()
	for _, name := range files {
		r.WriteFile(name, at+"\n")
	}
	r.Git(at, "add", "-A")
	r.Git(at, "commit", "--quiet", "--allow-empty", "-m", message)
	return r.Git(at, "rev-parse", "HEAD")
}
