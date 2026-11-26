package release

import "testing"

func TestVersion(t *testing.T) {
	tests := []struct {
		tag  string
		want string
		ok   bool
	}{
		{"v0.1.1", "v0.1.1", true},
		{"0.1.1", "v0.1.1", true},
		{"v1.0.0-rc.1", "v1.0.0-rc.1", true},
		{"v1.2.3+build.5", "v1.2.3+build.5", true},
		{"v1", "", false},
		{"v1.2", "", false},
		{"latest", "", false},
		{"release-1.0.0", "", false},
		{"v01.2.3", "v01.2.3", false},
	}
	for _, tt := range tests {
		got, ok := Version(tt.tag)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("Version(%q) = %q, %v; want %q, %v", tt.tag, got, ok, tt.want, tt.ok)
		}
	}
}

func TestLatest(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{[]string{"v0.0.9", "v0.0.10", "v0.0.2"}, "v0.0.10"},
		{[]string{"0.1.0", "0.1.1"}, "0.1.1"},
		{[]string{"v1.0.0-rc.1", "v0.9.0"}, "v1.0.0-rc.1"},
		{[]string{"v1.0.0", "v1.0.0-rc.1"}, "v1.0.0"},
		{[]string{"v2", "v1.9.9", "nightly"}, "v1.9.9"},
		{[]string{"nightly"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		got, ok := Latest(tt.tags)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("Latest(%v) = %q, %v; want %q", tt.tags, got, ok, tt.want)
		}
	}
}

func TestShipped(t *testing.T) {
	unshipped := []string{"docs/", "scripts/*.sh", "*.md"}
	tests := []struct {
		file string
		sdk  bool
		want bool
	}{
		{"main.go", false, true},
		{".github/workflows/ci.yml", false, false},
		{".github/dependabot.yml", true, false},
		{"go.sum", true, false},
		{"Tools/Package.resolved", true, false},
		{"go.sum", false, true},
		{"bun.lock", false, true},
		{"package.json", true, true},
		{"docs/guide/intro.txt", false, false},
		{"scripts/release.sh", false, false},
		{"scripts/nested/release.sh", false, true},
		{"README.md", false, false},
		{"src/notes/CHANGES.md", false, false},
		{"githubish/x.go", false, true},
	}
	for _, tt := range tests {
		if got := Shipped(tt.file, tt.sdk, unshipped); got != tt.want {
			t.Errorf("Shipped(%q, sdk=%v) = %v, want %v", tt.file, tt.sdk, got, tt.want)
		}
	}
}
