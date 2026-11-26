// Package doctor checks the public repos' hygiene: the pug-sdk topic, Dependabot's config,
// security updates and the OSV-Scanner workflow.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/pug-sh/leo/internal/gh"
)

const (
	sdkTopic   = "pug-sdk"
	issueRepo  = "leo"
	issueTitle = "Repo hygiene"
	actions    = "github-actions"
)

type Finding struct {
	Repo    string
	Problem string
}

// Run checks every repo in scope and keeps one issue in pug-sh/leo listing what they lack.
func Run(ctx context.Context, c *gh.Client) error {
	repos, err := c.Repos(ctx)
	if err != nil {
		return err
	}
	var findings []Finding
	var errs []error
	forbidden := false
	for _, r := range repos {
		problems, cantRead, err := check(ctx, c, r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		forbidden = forbidden || cantRead
		for _, p := range problems {
			c.Logf("%s: %s", r.Name, p)
			findings = append(findings, Finding{Repo: r.Name, Problem: p})
		}
	}
	if forbidden {
		c.Warnf("can't see whether Dependabot security updates are on: that takes the Administration (read) permission")
	}
	if err := errors.Join(errs...); err != nil {
		// A partial check would drop findings from the issue.
		return err
	}
	return report(ctx, c, findings)
}

// check returns what r lacks, and whether its security updates were unreadable.
func check(ctx context.Context, c *gh.Client, r gh.Repo) ([]string, bool, error) {
	var problems []string
	if strings.HasPrefix(r.Name, "sdk-") && !r.HasTopic(sdkTopic) {
		problems = append(problems, "has no `pug-sdk` topic, so Leo doesn't sync its protos")
	}
	root, err := c.RootEntries(ctx, r.Name)
	if err != nil {
		return nil, false, err
	}
	workflows, err := c.ActionsWorkflows(ctx, r.Name)
	if err != nil {
		return nil, false, err
	}
	hasWorkflows := slices.ContainsFunc(workflows, func(w gh.ActionsWorkflow) bool {
		return strings.HasPrefix(w.Path, ".github/workflows/")
	})
	ecosystems := Ecosystems(root, hasWorkflows)
	if len(ecosystems) == 0 {
		return problems, false, nil
	}

	config, err := c.File(ctx, r.Name, ".github/dependabot.yml")
	if err == nil && config == nil {
		config, err = c.File(ctx, r.Name, ".github/dependabot.yaml")
	}
	if err != nil {
		return nil, false, err
	}
	if config == nil {
		problems = append(problems, "has no `.github/dependabot.yml`")
	} else {
		problems = append(problems, DependabotProblems(config, ecosystems)...)
	}

	if !slices.ContainsFunc(ecosystems, func(e string) bool { return e != actions }) {
		return problems, false, nil
	}
	switch i := slices.IndexFunc(workflows, isOSV); {
	case i < 0:
		problems = append(problems, "has no OSV-Scanner workflow")
	case workflows[i].State != "active":
		problems = append(problems, fmt.Sprintf("has its OSV-Scanner workflow %s", strings.ReplaceAll(workflows[i].State, "_", " ")))
	}
	alerts, err := c.DependabotAlertsOn(ctx, r.Name)
	if err != nil {
		return nil, false, err
	}
	if !alerts {
		problems = append(problems, "has Dependabot alerts off")
	}
	updates, err := c.SecurityUpdatesOn(ctx, r.Name)
	if errors.Is(err, gh.ErrForbidden) {
		return problems, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !updates {
		problems = append(problems, "has Dependabot security updates off")
	}
	return problems, false, nil
}

func isOSV(w gh.ActionsWorkflow) bool {
	base := path.Base(w.Path)
	return w.Name == "OSV-Scanner" || base == "osv-scanner.yml" || base == "osv-scanner.yaml"
}

// Ecosystems names the Dependabot ecosystems a repo needs updates for, from the files at its root
// and whether it has workflows.
func Ecosystems(root []string, workflows bool) []string {
	has := func(names ...string) bool {
		return slices.ContainsFunc(names, func(n string) bool { return slices.Contains(root, n) })
	}
	var ecosystems []string
	add := func(ok bool, ecosystem string) {
		if ok {
			ecosystems = append(ecosystems, ecosystem)
		}
	}
	add(has("go.mod"), "gomod")
	add(has("bun.lock", "bun.lockb"), "bun")
	add(!has("bun.lock", "bun.lockb") && has("package.json", "package-lock.json"), "npm")
	add(has("Gemfile"), "bundler")
	add(has("pubspec.yaml"), "pub")
	add(has("build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"), "gradle")
	add(has("Package.swift"), "swift")
	add(has("Cargo.toml"), "cargo")
	add(has("uv.lock"), "uv")
	add(!has("uv.lock") && has("requirements.txt", "pyproject.toml"), "pip")
	add(has("Dockerfile"), "docker")
	add(workflows, actions)
	return ecosystems
}

type dependabotConfig struct {
	Updates []struct {
		Ecosystem string `yaml:"package-ecosystem"`
		Schedule  struct {
			Interval string `yaml:"interval"`
		} `yaml:"schedule"`
		Groups map[string]struct {
			UpdateTypes []string `yaml:"update-types"`
		} `yaml:"groups"`
	} `yaml:"updates"`
}

// DependabotProblems checks a dependabot.yml against the convention: each ecosystem updated
// monthly, with one group for its minor and patch updates.
func DependabotProblems(config []byte, ecosystems []string) []string {
	var cfg dependabotConfig
	if err := yaml.Unmarshal(config, &cfg); err != nil {
		return []string{fmt.Sprintf("has a `.github/dependabot.yml` that doesn't parse: %v", err)}
	}
	var problems []string
	add := func(p string) {
		if !slices.Contains(problems, p) {
			problems = append(problems, p)
		}
	}
	for _, eco := range ecosystems {
		found := false
		for _, u := range cfg.Updates {
			if u.Ecosystem != eco {
				continue
			}
			found = true
			if interval := strings.ToLower(u.Schedule.Interval); interval != "monthly" {
				add(fmt.Sprintf("has Dependabot check `%s` %s, not monthly", eco, interval))
			}
			grouped := false
			for _, g := range u.Groups {
				if slices.Contains(g.UpdateTypes, "minor") && slices.Contains(g.UpdateTypes, "patch") {
					grouped = true
				}
			}
			if !grouped {
				add(fmt.Sprintf("has no Dependabot group for `%s`'s minor and patch updates", eco))
			}
		}
		if !found {
			add(fmt.Sprintf("has Dependabot leave `%s` dependencies alone", eco))
		}
	}
	return problems
}

// report keeps the hygiene issue current: open while anything's left, closed once nothing is.
func report(ctx context.Context, c *gh.Client, findings []Finding) error {
	issues, err := c.LeoIssues(ctx, issueRepo, gh.MarkerDoctor)
	if err != nil {
		return err
	}
	var open *gh.Issue
	for i := range issues {
		if issues[i].Open {
			open = &issues[i]
			break
		}
	}
	if len(findings) == 0 {
		if open == nil {
			return nil
		}
		return c.CloseIssue(ctx, issueRepo, open.Number, "Nothing's left to fix, so I'm closing this.")
	}
	body := Body(findings)
	if open != nil {
		if open.Title == issueTitle && open.Body == body {
			return nil
		}
		return c.EditIssue(ctx, issueRepo, open.Number, issueTitle, body)
	}
	if len(issues) > 0 && issues[0].Body == body {
		// Someone closed the last one, and nothing has changed since.
		byPerson, err := c.ClosedByPerson(ctx, issueRepo, issues[0].Number)
		if err != nil || byPerson {
			return err
		}
	}
	_, err = c.CreateIssue(ctx, issueRepo, issueTitle, body)
	return err
}

func Body(findings []Finding) string {
	var b strings.Builder
	b.WriteString("`leo doctor` found these gaps in pug-sh's public repos.\n")
	repo := ""
	for _, f := range findings {
		if f.Repo != repo {
			repo = f.Repo
			fmt.Fprintf(&b, "\n**%s**\n\n", repo)
		}
		fmt.Fprintf(&b, "- %s\n", f.Problem)
	}
	b.WriteString("\nI'll keep this current, and close it once nothing's left.\n\n")
	b.WriteString(gh.Marker(gh.MarkerDoctor))
	return b.String()
}
