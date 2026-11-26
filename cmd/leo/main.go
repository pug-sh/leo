package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pug-sh/leo/internal/config"
	"github.com/pug-sh/leo/internal/gh"
	"github.com/pug-sh/leo/internal/inbox"
	"github.com/pug-sh/leo/internal/protos"
	"github.com/pug-sh/leo/internal/release"
	"github.com/pug-sh/leo/internal/security"
)

const usage = `usage: leo <command> [flags]

commands:
  setup         create Inbox if it's missing, and add any missing fields, options and views
  inbox         put open issues and pull requests on Inbox and fill their fields
  security      open an issue for each advisory whose alerts no pull request covers
  release       keep a reminder of each repo's unreleased changes
  protos plan   list the SDKs whose proto/ differs from pug's protos
  protos sync   push one SDK's codegen diff as a pull request

Every command takes --dry-run, which prints each write instead of making it.
The token comes from GH_TOKEN. Run leo <command> -h for a command's flags.`

var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	switch {
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "leo:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	name, args := args[0], args[1:]
	if name == "protos" {
		if len(args) == 0 {
			return errUsage
		}
		name, args = "protos "+args[0], args[1:]
	}
	fs := flag.NewFlagSet("leo "+name, flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print each write instead of making it")
	configPath := func() *string { return fs.String("config", "leo.yaml", "the per-repo overrides") }
	parse := func() (*gh.Client, error) {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() > 0 {
			return nil, errUsage
		}
		token := os.Getenv("GH_TOKEN")
		if token == "" {
			return nil, errors.New("GH_TOKEN is not set")
		}
		return gh.New(token, *dryRun, out)
	}

	switch name {
	case "setup":
		c, err := parse()
		if err != nil {
			return err
		}
		return inbox.Setup(ctx, c)

	case "inbox":
		c, err := parse()
		if err != nil {
			return err
		}
		return inbox.Run(ctx, c)

	case "security":
		c, err := parse()
		if err != nil {
			return err
		}
		return security.Run(ctx, c, security.NewOSV("https://api.osv.dev"), time.Now())

	case "release":
		path := configPath()
		c, err := parse()
		if err != nil {
			return err
		}
		cfg, err := config.Load(*path)
		if err != nil {
			return err
		}
		return release.New(c, cfg, time.Now()).Run(ctx)

	case "protos plan":
		path := configPath()
		buf := fs.String("buf", "buf", "the buf CLI")
		c, err := parse()
		if err != nil {
			return err
		}
		cfg, err := config.Load(*path)
		if err != nil {
			return err
		}
		bsr, targets, err := protos.NewPlanner(c, cfg, *buf).Plan(ctx)
		if bsr.Commit == "" {
			return err
		}
		// The SDKs that could be checked still sync when another couldn't.
		return errors.Join(err, writePlan(out, bsr, targets))

	case "protos sync":
		var in protos.SyncInput
		fs.StringVar(&in.Repo, "repo", "", "the SDK, as a repo name")
		fs.StringVar(&in.Branch, "branch", "main", "the SDK's default branch")
		fs.StringVar(&in.Base, "base", "", "the commit of the branch the codegen job ran on")
		fs.StringVar(&in.Dir, "dir", "", "a checkout of the base commit")
		fs.StringVar(&in.Patch, "patch", "", "the codegen job's diff")
		fs.StringVar(&in.Log, "log", "", "the end of the codegen job's log")
		fs.StringVar(&in.BSR.Commit, "commit", "", "the BSR commit to sync to")
		fs.StringVar(&in.BSR.SourceControlURL, "source", "", "the pug commit the BSR commit came from")
		fs.StringVar(&in.Codegen, "codegen", "", "the codegen job's result: success, failure, cancelled or skipped")
		c, err := parse()
		if err != nil {
			return err
		}
		if in.Repo == "" || in.BSR.Commit == "" || in.Codegen == "" || (in.Codegen == "success" && (in.Base == "" || in.Dir == "" || in.Patch == "")) {
			return errors.New("leo protos sync needs --repo, --commit and --codegen, and after a successful codegen job --base, --dir and --patch")
		}
		in.RunURL = runURL()
		return protos.Sync(ctx, c, in)
	}
	return errUsage
}

// writePlan prints the sync matrix and, in a workflow, sets it as the step's outputs.
func writePlan(out io.Writer, bsr protos.BSRCommit, targets []protos.Target) error {
	matrix, err := json.Marshal(append([]protos.Target{}, targets...))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "matrix: %s\n", matrix)
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "commit=%s\nsource=%s\nmatrix=%s\n", bsr.Commit, bsr.SourceControlURL, matrix)
	return errors.Join(err, f.Close())
}

func runURL() string {
	server, repo, id := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || id == "" {
		return ""
	}
	return server + "/" + repo + "/actions/runs/" + id
}
