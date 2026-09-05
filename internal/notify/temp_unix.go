//go:build unix

package notify

import (
	"os"
	"path/filepath"
)

func notificationTempRoot() (string, error) {
	resolved, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return filepath.Clean(os.TempDir()), nil
	}
	return resolved, nil
}
