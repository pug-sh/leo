package protos

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseMakefile(t *testing.T) {
	sdkPaths := []string{"sdk", "common/events/v1", "common/v1/property_value.proto"}
	serverPaths := append(slices.Clone(sdkPaths), "shared/activity/v1", "shared/insights/v1", "shared/profiles/v1")
	mobilePaths := []string{"sdk/events/v1", "sdk/profiles/v1", "common/events/v1", "common/v1/property_value.proto"}

	tests := []struct {
		repo   string
		commit string
		paths  []string
		target string
	}{
		{"sdk-node", "35d2a94a2a654a45a773e9184e3f6324", serverPaths, "protos"},
		{"sdk-browser", "432c927edc274179ad23f1498a6ad20d", sdkPaths, "protos"},
		{"sdk-crawlers", "c78730668928485697067836bcbc4a1b", []string{"common/crawlers/v1", "sdk/events/v1"}, "codegen"},
		{"sdk-go", "8b5d0680b10c4693b4d9d15d3dbfc663", serverPaths, "protos"},
		{"sdk-ruby", "35d2a94a2a654a45a773e9184e3f6324", serverPaths, "protos"},
		{"sdk-flutter", "4f07eb37ba8443a398ec5f1f6403827e", sdkPaths, "protos"},
		{"sdk-android", "8b5d0680b10c4693b4d9d15d3dbfc663", mobilePaths, "protos"},
		{"sdk-ios", "8b5d0680b10c4693b4d9d15d3dbfc663", mobilePaths, "protos"},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tt.repo + ".Makefile")
			if err != nil {
				t.Fatal(err)
			}
			mk, err := ParseMakefile(data)
			if err != nil {
				t.Fatal(err)
			}
			if mk.Module != Module {
				t.Errorf("Module = %q", mk.Module)
			}
			if mk.Commit != tt.commit {
				t.Errorf("Commit = %q, want %q", mk.Commit, tt.commit)
			}
			if !slices.Equal(mk.Paths, tt.paths) {
				t.Errorf("Paths = %q, want %q", mk.Paths, tt.paths)
			}
			if mk.Target != tt.target {
				t.Errorf("Target = %q, want %q", mk.Target, tt.target)
			}
		})
	}
}

func TestParseMakefileErrors(t *testing.T) {
	_, err := ParseMakefile([]byte("build:\n\tgo build ./...\n"))
	if err == nil {
		t.Fatal("parsed a Makefile without the SDK contract")
	}
	for _, want := range []string{"PROTO_COMMIT", "PROTO_MODULE", "--path", "protos or codegen"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't mention %s", err, want)
		}
	}
}

func TestParseMakefileVariants(t *testing.T) {
	mk, err := ParseMakefile([]byte("PROTO_MODULE = buf.build/pugsh/pug # the module\r\n" +
		"PROTO_COMMIT ?= abc123   # pinned\r\n" +
		"sync-protos: deps\r\n" +
		"\t# Don't read this --path note\r\n" +
		"\t@buf export $(PROTO_MODULE):$(PROTO_COMMIT) --output proto \\\r\n" +
		"\t  --path=sdk --path 'common/v1'\r\n" +
		"protos codegen: sync-protos\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if mk.Module != Module || mk.Commit != "abc123" || mk.Target != "protos" {
		t.Errorf("parsed %+v", mk)
	}
	if !slices.Equal(mk.Paths, []string{"sdk", "common/v1"}) {
		t.Errorf("Paths = %q", mk.Paths)
	}
}
