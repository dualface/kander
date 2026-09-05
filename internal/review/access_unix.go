//go:build unix

package review

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func reviewTempRoot() (string, error) {
	resolved, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return filepath.Clean(os.TempDir()), nil
	}
	return resolved, nil
}

func dirReadableWritable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	return unix.Access(path, unix.R_OK|unix.W_OK) == nil
}
