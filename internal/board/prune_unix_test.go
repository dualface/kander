//go:build unix

package board

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A top-level symlink already fails the directory listing, so a FIFO covers
// the refusal of a listed non-regular entry beside run.json.
func TestRemovePathRefusesTopLevelSpecialFile(t *testing.T) {
	requireRemovePathRefuses(t, func(dir string) error {
		return unix.Mkfifo(filepath.Join(dir, "fifo"), 0o600)
	})
}
