package main

import (
	"context"
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
	"github.com/pug-sh/leo/internal/release"
	"github.com/pug-sh/leo/internal/security"
)

const usage = `usage: leo <command> [flags]

commands:
  setup      create Inbox if it's missing, and add any missing fields, options and views
  inbox      put open issues and pull requests on Inbox and fill their fields
  security   open an issue for each advisory whose alerts no pull request covers
  release    keep a reminder of each repo's unreleased changes

Every command takes --dry-run, which prints each write instead of making it.
The token comes from GH_TOKEN.`

var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	switch {
	case errors.Is(err, errUsage) || errors.Is(err, flag.ErrHelp):
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
	fs := flag.NewFlagSet("leo "+name, flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print each write instead of making it")
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
		configPath := fs.String("config", "leo.yaml", "the per-repo overrides")
		c, err := parse()
		if err != nil {
			return err
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		return release.New(c, cfg, time.Now()).Run(ctx)
	}
	return errUsage
}
