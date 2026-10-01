package launch

import (
	"os"
	"path/filepath"
)

type sessionDirectoryMismatch struct{ error }

// matchesSessionDirectory compares physical launch directories, allowing path
// aliases without treating a task ID as globally unique across repositories.
func matchesSessionDirectory(actual, expected string) bool {
	if !filepath.IsAbs(actual) {
		return false
	}
	want, err := os.Stat(expected)
	if err != nil || !want.IsDir() {
		return false
	}
	got, err := os.Stat(actual)
	return err == nil && got.IsDir() && os.SameFile(got, want)
}
