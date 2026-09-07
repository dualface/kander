//go:build !windows

package board

import (
	"github.com/dualface/kander/internal/fs"
	"testing"
)

func holdJournalTemporary(t *testing.T, root string) func() {
	t.Helper()
	path := control(root, "operations", ".publication.tmp")
	handle, err := fs.OpenLockFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
		if _, err := fs.RemoveRegularFileIfExists(root, path); err != nil {
			t.Error(err)
		}
	}
}
