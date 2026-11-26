package config

import (
	"slices"
	"testing"
)

func TestLeoYAMLParses(t *testing.T) {
	cfg, err := Load("../../leo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Repo("sdk-ruby").Unshipped; !slices.Contains(got, "Gemfile") {
		t.Errorf("sdk-ruby unshipped = %v", got)
	}
	if got := cfg.Repo("no-such-repo"); got.Unshipped != nil {
		t.Errorf("unknown repo = %+v", got)
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	if _, err := Parse([]byte("repos:\n  pug:\n    unshiped: [docs/]\n")); err == nil {
		t.Error("a misspelled key parsed")
	}
}
