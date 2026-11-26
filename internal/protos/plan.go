package protos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pug-sh/leo/internal/config"
	"github.com/pug-sh/leo/internal/gh"
	"github.com/pug-sh/leo/internal/git"
)

const (
	Module   = "buf.build/pugsh/pug"
	sdkTopic = "pug-sdk"
)

// BSRCommit is buf registry module commit resolve's answer for the module's main.
type BSRCommit struct {
	Commit           string `json:"commit"`
	SourceControlURL string `json:"source_control_url"`
}

func (b BSRCommit) Short() string { return b.Commit[:min(8, len(b.Commit))] }

// Target is a drifted SDK: one row of sync.yml's matrix.
type Target struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	Toolchain string `json:"toolchain"`
	Target    string `json:"target"`
	Bun       string `json:"bun"`
	Node      string `json:"node"`
	Protoc    string `json:"protoc"`
}

type Planner struct {
	c        *gh.Client
	cfg      *config.Config
	cloneURL func(repo string) string
	resolve  func(ctx context.Context) (BSRCommit, error)
	export   func(ctx context.Context, commit string, paths []string, out string) error
}

func NewPlanner(c *gh.Client, cfg *config.Config, buf string) *Planner {
	return &Planner{
		c:   c,
		cfg: cfg,
		cloneURL: func(repo string) string {
			return "https://github.com/" + gh.Org + "/" + repo + ".git"
		},
		resolve: func(ctx context.Context) (BSRCommit, error) {
			out, err := run(ctx, buf, "registry", "module", "commit", "resolve", Module+":main", "--format", "json")
			if err != nil {
				return BSRCommit{}, err
			}
			var b BSRCommit
			if err := json.Unmarshal(out, &b); err != nil || b.Commit == "" {
				return BSRCommit{}, fmt.Errorf("buf registry module commit resolve: unexpected output %q", out)
			}
			return b, nil
		},
		export: func(ctx context.Context, commit string, paths []string, out string) error {
			args := []string{"export", Module + ":" + commit, "--output", out}
			for _, p := range paths {
				args = append(args, "--path", p)
			}
			_, err := run(ctx, buf, args...)
			return err
		},
	}
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Plan resolves the module's main and returns the SDKs whose proto/ differs from it. An SDK it
// can't check gets a sync failure issue instead, and an SDK in sync has its failure issue closed.
func (p *Planner) Plan(ctx context.Context) (BSRCommit, []Target, error) {
	repos, err := p.c.Repos(ctx)
	if err != nil {
		return BSRCommit{}, nil, err
	}
	bsr, err := p.resolve(ctx)
	if err != nil {
		return BSRCommit{}, nil, err
	}
	p.c.Logf("%s:main is %s, from %s", Module, bsr.Commit, bsr.SourceControlURL)
	var targets []Target
	var errs []error
	for _, r := range repos {
		if !r.HasTopic(sdkTopic) {
			continue
		}
		target, drift, err := p.check(ctx, r, bsr)
		switch {
		case err != nil:
			p.c.Warnf("%s: %v", r.Name, err)
			failure := Failure{Repo: r.Name, BSR: bsr, What: "I couldn't check its protos", Detail: err.Error()}
			if err := ReportFailure(ctx, p.c, failure); err != nil {
				errs = append(errs, err)
			}
		case drift == 0:
			p.c.Logf("%s: proto/ matches %s", r.Name, bsr.Short())
			if err := CloseFailures(ctx, p.c, r.Name, fmt.Sprintf("proto/ matches `%s` now, so I'm closing this.", bsr.Short())); err != nil {
				errs = append(errs, err)
			}
		default:
			files := "files differ"
			if drift == 1 {
				files = "file differs"
			}
			p.c.Logf("%s: %d %s in proto/ from %s", r.Name, drift, files, bsr.Short())
			targets = append(targets, target)
		}
	}
	return bsr, targets, errors.Join(errs...)
}

func (p *Planner) check(ctx context.Context, r gh.Repo, bsr BSRCommit) (Target, int, error) {
	dir, err := os.MkdirTemp("", "leo-protos-")
	if err != nil {
		return Target{}, 0, err
	}
	defer os.RemoveAll(dir)
	sdk := filepath.Join(dir, "sdk")
	// Cone mode always checks out the root's files, so the Makefile comes with proto/.
	if _, err := git.Run(ctx, "", "clone", "--quiet", "--depth=1", "--filter=blob:none", "--sparse", "--branch", r.DefaultBranch, p.cloneURL(r.Name), sdk); err != nil {
		return Target{}, 0, err
	}
	if _, err := git.Run(ctx, sdk, "sparse-checkout", "set", "proto"); err != nil {
		return Target{}, 0, err
	}
	data, err := os.ReadFile(filepath.Join(sdk, "Makefile"))
	if err != nil {
		return Target{}, 0, err
	}
	mk, err := ParseMakefile(data)
	if err != nil {
		return Target{}, 0, err
	}
	if mk.Module != Module {
		return Target{}, 0, fmt.Errorf("its Makefile vendors %s, not %s", mk.Module, Module)
	}
	root, err := git.Lines(ctx, sdk, "ls-tree", "--name-only", "HEAD")
	if err != nil {
		return Target{}, 0, err
	}
	toolchain := DetectToolchain(root)
	if toolchain == "" {
		return Target{}, 0, errors.New("no toolchain for its codegen: sync.yml sets up bun, go, ruby, flutter, gradle and swift SDKs")
	}
	exported := filepath.Join(dir, "export")
	if err := p.export(ctx, bsr.Commit, mk.Paths, exported); err != nil {
		return Target{}, 0, err
	}
	drift, err := diffTrees(exported, filepath.Join(sdk, "proto"))
	if err != nil {
		return Target{}, 0, err
	}
	codegen := p.cfg.Repo(r.Name).Codegen
	return Target{
		Repo:      r.Name,
		Branch:    r.DefaultBranch,
		Toolchain: toolchain,
		Target:    mk.Target,
		Bun:       codegen.Bun,
		Node:      codegen.Node,
		Protoc:    codegen.Protoc,
	}, drift, nil
}

// DetectToolchain names the codegen toolchain from the files at an SDK's root. sync.yml has one
// setup block for each.
func DetectToolchain(root []string) string {
	for _, tc := range []struct{ file, toolchain string }{
		{"Package.swift", "swift"},
		{"gradlew", "gradle"},
		{"pubspec.yaml", "flutter"},
		{"go.mod", "go"},
		{"Gemfile", "ruby"},
		{"bun.lock", "bun"},
	} {
		if slices.Contains(root, tc.file) {
			return tc.toolchain
		}
	}
	return ""
}

// diffTrees counts the files that differ between two directories, either missing one side or
// with different content.
func diffTrees(a, b string) (int, error) {
	fa, err := readTree(a)
	if err != nil {
		return 0, err
	}
	fb, err := readTree(b)
	if err != nil {
		return 0, err
	}
	n := 0
	for name, data := range fa {
		if other, ok := fb[name]; !ok || !bytes.Equal(data, other) {
			n++
		}
	}
	for name := range fb {
		if _, ok := fa[name]; !ok {
			n++
		}
	}
	return n, nil
}

func readTree(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == root {
			return fs.SkipAll
		}
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = data
		return err
	})
	return files, err
}
