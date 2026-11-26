package release

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pug-sh/leo/internal/git"
)

type Commit struct {
	SHA     string
	Time    time.Time
	Subject string
	Files   []string
	// PR is the pull request a merge or squash commit brought in, and Title its title.
	PR    int
	Title string
}

var (
	mergeRE  = regexp.MustCompile(`^Merge pull request #(\d+) from \S+`)
	squashRE = regexp.MustCompile(`\s*\(#(\d+)\)$`)
)

// FirstParent lists the first-parent commits in from..to, oldest first, each with the files it
// changed against its first parent.
func FirstParent(ctx context.Context, dir, from, to string) ([]Commit, error) {
	out, err := git.Run(ctx, dir, "log", "--first-parent", "--diff-merges=first-parent", "--name-only", "-z",
		"--format=%x1e%H%x1f%ct%x1f%s%x1f%b%x1f", "--end-of-options", from+".."+to)
	if err != nil {
		return nil, err
	}
	commits, err := parseLog(string(out))
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

// parseLog reads records of "\x1e" hash, time, subject and body, each followed by "\x1f", then
// the NUL-separated names of the changed files.
func parseLog(out string) ([]Commit, error) {
	var commits []Commit
	for record := range strings.SplitSeq(out, "\x1e") {
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("git log: malformed record %q", record)
		}
		sec, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git log: bad time %q", fields[1])
		}
		c := Commit{SHA: fields[0], Time: time.Unix(sec, 0).UTC(), Subject: fields[2], Title: fields[2]}
		for name := range strings.SplitSeq(fields[4], "\x00") {
			if name = strings.TrimLeft(name, "\n"); name != "" {
				c.Files = append(c.Files, name)
			}
		}
		if m := mergeRE.FindStringSubmatch(c.Subject); m != nil {
			c.PR, _ = strconv.Atoi(m[1])
			if title, _, _ := strings.Cut(strings.TrimSpace(fields[3]), "\n"); title != "" {
				c.Title = title
			}
		} else if m := squashRE.FindStringSubmatch(c.Subject); m != nil {
			c.PR, _ = strconv.Atoi(m[1])
			c.Title = strings.TrimSuffix(c.Subject, m[0])
		}
		commits = append(commits, c)
	}
	return commits, nil
}
