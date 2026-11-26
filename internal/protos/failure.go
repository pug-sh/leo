package protos

import (
	"context"
	"fmt"
	"strings"

	"github.com/pug-sh/leo/internal/gh"
)

const failureTitle = "Proto sync failed"

// Failure is a sync that failed for one SDK.
type Failure struct {
	Repo   string
	BSR    BSRCommit
	What   string
	Detail string
	RunURL string
}

// ReportFailure opens or updates the SDK's sync failure issue. If someone closed the last one,
// only a failure at a newer pug commit opens another.
func ReportFailure(ctx context.Context, c *gh.Client, f Failure) error {
	issues, err := c.LeoIssues(ctx, f.Repo, gh.MarkerProtosFailed)
	if err != nil {
		return err
	}
	body := failureBody(f)
	for _, is := range issues {
		if !is.Open {
			continue
		}
		if is.Title == failureTitle && is.Body == body {
			return nil
		}
		return c.EditIssue(ctx, f.Repo, is.Number, failureTitle, body)
	}
	if len(issues) > 0 {
		last := issues[0]
		byPerson, err := c.ClosedByPerson(ctx, f.Repo, last.Number)
		if err != nil {
			return err
		}
		if args, _ := gh.ParseMarker(last.Body, gh.MarkerProtosFailed); byPerson && len(args) > 0 && args[0] == f.BSR.Commit {
			c.Logf("%s: someone closed #%d, the failure at %s, so I've left it closed", f.Repo, last.Number, f.BSR.Short())
			return nil
		}
	}
	_, err = c.CreateIssue(ctx, f.Repo, failureTitle, body)
	return err
}

// CloseFailures closes the SDK's open sync failure issues.
func CloseFailures(ctx context.Context, c *gh.Client, repo, comment string) error {
	issues, err := c.LeoIssues(ctx, repo, gh.MarkerProtosFailed)
	if err != nil {
		return err
	}
	for _, is := range issues {
		if is.Open {
			if err := c.CloseIssue(ctx, repo, is.Number, comment); err != nil {
				return err
			}
		}
	}
	return nil
}

func failureBody(f Failure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Syncing protos to `%s` failed: %s.", f.BSR.Short(), f.What)
	if f.BSR.SourceControlURL != "" {
		fmt.Fprintf(&b, " The protos come from pug's [%s](%s).", pugCommit(f.BSR), f.BSR.SourceControlURL)
	}
	if f.RunURL != "" {
		fmt.Fprintf(&b, " [The run](%s) has the whole log.", f.RunURL)
	}
	b.WriteString("\n\n")
	if f.Detail != "" {
		fence := codeFence(f.Detail)
		fmt.Fprintf(&b, "<details>\n<summary>End of the log</summary>\n\n%stext\n%s\n%s\n\n</details>\n\n", fence, strings.TrimRight(f.Detail, "\n"), fence)
	}
	b.WriteString("I'll close this after the next sync succeeds.\n\n")
	b.WriteString(gh.Marker(gh.MarkerProtosFailed, f.BSR.Commit))
	return b.String()
}

// pugCommit is the short pug commit a BSR commit came from.
func pugCommit(b BSRCommit) string {
	sha := b.SourceControlURL[strings.LastIndex(b.SourceControlURL, "/")+1:]
	return "commit " + sha[:min(8, len(sha))]
}

// codeFence is longer than any run of backticks in text, so text can't close it.
func codeFence(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// Tail keeps the last lines of a log, and at most maxBytes of them, to stay inside GitHub's
// limit on an issue's length.
func Tail(log string, lines, maxBytes int) string {
	log = strings.TrimRight(log, "\n")
	all := strings.Split(log, "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	tail := strings.Join(all, "\n")
	if len(tail) > maxBytes {
		tail = tail[len(tail)-maxBytes:]
		if i := strings.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
	}
	return tail
}
