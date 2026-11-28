package svcinstall

import (
	"os"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/fs"
)

// readDefinition reads a definition file of a manager directory. A path that is not a regular
// file (a link a dotfile manager placed, say) is found without content, so it counts as a
// definition Kander does not own and is never followed or changed.
func readDefinition(dir, path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || fs.IsReparsePoint(path) {
		return nil, true, nil
	}
	data, found, err := fs.ReadRegularFileIfExists(dir, path)
	return data, found, err
}

// writeDefinition publishes a definition file atomically, creating the manager directory.
func writeDefinition(dir, path string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fs.WriteTextAtomicInherited(dir, path, string(data), true)
}

// removeDefinition deletes a definition file without following a link.
func removeDefinition(dir, path string) error {
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil
	}
	_, err := fs.RemoveRegularFileIfExists(dir, filepath.Join(dir, filepath.Base(path)))
	return err
}
