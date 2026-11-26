package release

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

// x/mod/semver also takes v1 and v1.2, which a moving major tag could use, so a release tag must
// have all three numbers.
var releaseTagRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+([-+].*)?$`)

// Version returns tag as a semver version with a v, if tag is a release tag: semver with an
// optional v, as sdk-ios's 0.1.1 is.
func Version(tag string) (string, bool) {
	if !releaseTagRE.MatchString(tag) {
		return "", false
	}
	v := "v" + strings.TrimPrefix(tag, "v")
	return v, semver.IsValid(v)
}

// Latest returns the highest release tag among tags.
func Latest(tags []string) (string, bool) {
	var best, bestV string
	for _, tag := range tags {
		v, ok := Version(tag)
		if !ok {
			continue
		}
		if best == "" || semver.Compare(v, bestV) > 0 || (semver.Compare(v, bestV) == 0 && tag > best) {
			best, bestV = tag, v
		}
	}
	return best, best != ""
}

var lockfiles = []string{
	"bun.lock", "package-lock.json", "go.sum", "Gemfile.lock", "Cargo.lock", "Package.resolved", "pubspec.lock", "uv.lock",
}

// Shipped reports whether file reaches users: everything but .github/, lockfiles in SDKs, which
// are libraries, and the repo's unshipped patterns from leo.yaml.
func Shipped(file string, sdk bool, unshipped []string) bool {
	if strings.HasPrefix(file, ".github/") {
		return false
	}
	if sdk && slices.Contains(lockfiles, path.Base(file)) {
		return false
	}
	return !slices.ContainsFunc(unshipped, func(pattern string) bool { return match(pattern, file) })
}

// match matches a directory when pattern ends in /, the whole path when it has a /, and a file
// name anywhere otherwise.
func match(pattern, file string) bool {
	switch {
	case strings.HasSuffix(pattern, "/"):
		return strings.HasPrefix(file, pattern)
	case strings.Contains(pattern, "/"):
		ok, _ := path.Match(pattern, file)
		return ok
	}
	ok, _ := path.Match(pattern, path.Base(file))
	return ok
}
