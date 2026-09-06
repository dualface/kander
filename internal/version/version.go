// Package version exposes the Kander version identity injected at build time.
package version

import "strings"

var (
	// BuildTimestamp is injected by the build entry point in UTC YYYYMMDDTHHMMSSZ format.
	BuildTimestamp = "dev"
	// GitHash is injected by the build entry point as the short hash of the current commit.
	GitHash = "unknown"
)

func component(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return strings.Join(strings.Fields(value), "_")
}

// String returns the stable <build-timestamp>-<git-hash> version.
func String() string {
	return component(BuildTimestamp, "dev") + "-" + component(GitHash, "unknown")
}
