package gh

import (
	"regexp"
	"strings"
)

// Leo finds its own issues and comments by these hidden markers, and only trusts them on what it
// authored, since anyone can paste one into a public issue.
const (
	MarkerSecurity     = "leo:security"
	MarkerRelease      = "leo:release"
	MarkerProtos       = "leo:protos"
	MarkerProtosFailed = "leo:protos-failed"
	MarkerProtosLocked = "leo:protos-locked"
	MarkerDoctor       = "leo:doctor"
)

func Marker(name string, args ...string) string {
	return "<!-- " + strings.Join(append([]string{name}, args...), " ") + " -->"
}

var markerRE = regexp.MustCompile(`<!--\s*(leo:[a-z-]+)((?:\s+[^\s>]+)*)\s*-->`)

// ParseMarker finds the first marker called name in body and returns its arguments.
func ParseMarker(body, name string) ([]string, bool) {
	for _, m := range markerRE.FindAllStringSubmatch(body, -1) {
		if m[1] == name {
			return strings.Fields(m[2]), true
		}
	}
	return nil, false
}

func HasMarker(body, name string) bool {
	_, ok := ParseMarker(body, name)
	return ok
}
