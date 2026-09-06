//go:build unix

package fs

import "os"

// IsReparsePoint reports whether the path is a symlink, without following it.
func IsReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}
