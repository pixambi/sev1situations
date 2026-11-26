// Package version holds the build identity of the application.
//
// The three variables are stamped at build time with -ldflags (see the build
// tasks in Taskfile.yml). An unstamped build, such as `task dev` or `go run`,
// reports the version "dev", which is deliberately not a release version.
package version

import (
	"fmt"
	"regexp"
)

// Set by -ldflags "-X github.com/pixambi/sev1situations/internal/version.Version=1.2.3".
var (
	// Version is the semantic version without a leading "v", or "dev".
	Version = "dev"
	// Commit is the short git commit the binary was built from.
	Commit = "none"
	// Date is the UTC build time in RFC 3339 form.
	Date = "unknown"
)

// semverPattern is the official expression from https://semver.org, anchored.
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// IsSemver reports whether v is a valid semantic version. A leading "v" is not
// accepted: versions are stored and compared without it.
func IsSemver(v string) bool {
	return semverPattern.MatchString(v)
}

// IsRelease reports whether this binary was stamped with a semantic version,
// which is what makes it eligible for self-update.
func IsRelease() bool {
	return IsSemver(Version)
}

// String returns a one-line description, as printed by `s1s version`.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
