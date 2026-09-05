//go:build windows

package review

import (
	"os"
	"path/filepath"
)

func reviewTempRoot() (string, error) {
	return filepath.Clean(os.TempDir()), nil
}

func dirReadableWritable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	probe, err := os.CreateTemp(path, ".kander-rw-")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}
