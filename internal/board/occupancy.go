package board

import (
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// HasTaskEntries reports whether a board directory holds any task entry. It
// only reads: unlike LoadBoard it creates no control directory or lock file
// and accepts a partly initialized or missing board. Every non-hidden member
// of a state directory counts, so an unknown entry reads as occupied.
func HasTaskEntries(root string) (bool, error) {
	parent := filepath.Dir(root)
	if exists, err := fs.DirectoryExists(parent, root); err != nil || !exists {
		return false, err
	}
	for _, state := range States {
		dir := filepath.Join(root, state)
		exists, err := fs.DirectoryExists(root, dir)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		entries, err := fs.ListDirectory(root, dir)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name, ".") {
				return true, nil
			}
		}
	}
	return false, nil
}
