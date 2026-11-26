package gh

import (
	"slices"
	"testing"
)

func TestParseMarker(t *testing.T) {
	body := "Text.\n\n" + Marker(MarkerSecurity, "GHSA-aaaa-bbbb-cccc") + "\n" + Marker(MarkerRelease)
	if args, ok := ParseMarker(body, MarkerSecurity); !ok || !slices.Equal(args, []string{"GHSA-aaaa-bbbb-cccc"}) {
		t.Errorf("security marker = %v, %v", args, ok)
	}
	if args, ok := ParseMarker(body, MarkerRelease); !ok || len(args) != 0 {
		t.Errorf("release marker = %v, %v", args, ok)
	}
	if _, ok := ParseMarker(body, MarkerProtosFailed); ok {
		t.Error("found a marker that isn't there")
	}
	if _, ok := ParseMarker("<!-- leo:securityx GHSA -->", MarkerSecurity); ok {
		t.Error("matched a longer marker name")
	}
	if args, _ := ParseMarker("<!--leo:protos-failed abc   -->", MarkerProtosFailed); !slices.Equal(args, []string{"abc"}) {
		t.Errorf("loose spacing: %v", args)
	}
}

func TestNormalizeLogin(t *testing.T) {
	tests := []struct{ login, typename, want string }{
		{"dependabot", "Bot", "dependabot[bot]"},
		{"leo-the-pug[bot]", "Bot", "leo-the-pug[bot]"},
		{"poluruprvn", "User", "poluruprvn"},
	}
	for _, tt := range tests {
		if got := NormalizeLogin(tt.login, tt.typename); got != tt.want {
			t.Errorf("NormalizeLogin(%q, %q) = %q, want %q", tt.login, tt.typename, got, tt.want)
		}
	}
}
