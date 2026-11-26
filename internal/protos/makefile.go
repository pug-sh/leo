package protos

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Makefile is what Leo reads from an SDK's Makefile, which must have a PROTO_COMMIT := <commit>
// line, the --path arguments in PROTO_PATHS or in the sync-protos recipe, and a protos or codegen
// target.
type Makefile struct {
	Module string
	Commit string
	Paths  []string
	Target string
}

var (
	assignRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:::=|:=|\?=|\+=|=)\s*(.*)$`)
	ruleRE   = regexp.MustCompile(`^([^:=#\t][^:=#]*?)\s*::?(?:[^=]|$)`)
)

func ParseMakefile(data []byte) (Makefile, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\\\n", " ")

	vars := map[string]string{}
	var targets []string
	recipes := map[string][]string{}
	var current []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "\t") {
			for _, t := range current {
				recipes[t] = append(recipes[t], line[1:])
			}
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := assignRE.FindStringSubmatch(line); m != nil {
			vars[m[1]] = stripComment(m[2])
			current = nil
			continue
		}
		if m := ruleRE.FindStringSubmatch(line); m != nil {
			current = strings.Fields(m[1])
			targets = append(targets, current...)
			continue
		}
		current = nil
	}

	mk := Makefile{Module: firstField(vars["PROTO_MODULE"]), Commit: firstField(vars["PROTO_COMMIT"])}
	if paths, ok := vars["PROTO_PATHS"]; ok {
		mk.Paths = pathArgs(paths)
	} else {
		for _, line := range recipes["sync-protos"] {
			line = strings.TrimLeft(line, "@-+ \t")
			if !strings.HasPrefix(line, "#") {
				mk.Paths = append(mk.Paths, pathArgs(line)...)
			}
		}
	}
	switch {
	case slices.Contains(targets, "protos"):
		mk.Target = "protos"
	case slices.Contains(targets, "codegen"):
		mk.Target = "codegen"
	}

	var errs []error
	if mk.Commit == "" {
		errs = append(errs, errors.New("no PROTO_COMMIT line"))
	}
	if mk.Module == "" {
		errs = append(errs, errors.New("no PROTO_MODULE line"))
	}
	if len(mk.Paths) == 0 {
		errs = append(errs, errors.New("no --path arguments in PROTO_PATHS or the sync-protos recipe"))
	}
	if mk.Target == "" {
		errs = append(errs, errors.New("no protos or codegen target"))
	}
	if err := errors.Join(errs...); err != nil {
		return mk, fmt.Errorf("Makefile: %w", err)
	}
	return mk, nil
}

func stripComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func firstField(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func pathArgs(s string) []string {
	var paths []string
	fields := strings.Fields(s)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "--path" && i+1 < len(fields):
			i++
			paths = append(paths, strings.Trim(fields[i], `'"`))
		case strings.HasPrefix(f, "--path="):
			paths = append(paths, strings.Trim(strings.TrimPrefix(f, "--path="), `'"`))
		}
	}
	return paths
}
