package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/fs"
)

// readJSON loads one private JSON record below root. A missing file reports
// found=false; every other failure, including malformed JSON, is an error so
// a damaged control record never reads as absent.
func readJSON(root, rel string, value any) (found bool, err error) {
	data, ok, err := fs.ReadRegularFileIfExists(root, filepath.Join(root, rel))
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return true, errors.Join(errors.New("control record "+rel+" is damaged"), err)
	}
	return true, nil
}

// writeJSON replaces one record atomically relative to its pinned parent.
func writeJSON(root, rel string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, rel)
	if err := fs.EnsurePrivateDirectory(root, filepath.Dir(path), true); err != nil {
		return err
	}
	return fs.WriteTextAtomic(root, path, string(data)+"\n", true)
}

// ensureDir creates a private directory chain below root.
func ensureDir(root, rel string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	return fs.EnsurePrivateDirectory(root, filepath.Join(root, rel), true)
}
