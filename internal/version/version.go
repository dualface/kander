// Package version exposes the Kander version identity injected at build time.
package version

import "strings"

// Version is injected by the build entry point as the semantic version.
// The default is used for untagged local builds and checkouts outside git.
var Version = "dev"

// String returns the semantic version, or "dev" when Version is blank.
func String() string {
	value := strings.TrimSpace(Version)
	if value == "" {
		return "dev"
	}
	return value
}
