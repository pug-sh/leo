// Package git runs the git CLI.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run runs git in dir and returns its standard output.
func Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Lines runs git and splits its output into non-empty lines.
func Lines(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}
