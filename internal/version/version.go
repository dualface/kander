// Package version exposes the Kander version identity injected at build time.
package version

import "strings"

// Version is injected by the build entry point.
// Release builds inject the tag with the leading v stripped. Local make
// injects `git describe --tags --always` when a tag is reachable, and
// "dev" when the checkout has no tags or is not a git repository.
var Version = "dev"

// String returns the injected version, or "dev" when Version is blank.
func String() string {
	value := strings.TrimSpace(Version)
	if value == "" {
		return "dev"
	}
	return value
}
