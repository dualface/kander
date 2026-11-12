package server

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

func TestImportSnapshotPreservesFilesWhileTheSourceLockIsHeld(t *testing.T) {
	root := t.TempDir()
	if err := board.InitStorage(root); err != nil {
		t.Fatal(err)
	}
	if err := board.RecoverTransactions(root); err != nil {
		t.Fatal(err)
	}
	// A nonempty file with the same suffix must retain its actual bytes too.
	const other = ".kander/locks/evidence.lock"
	if err := fs.WriteTextAtomic(root, filepath.Join(root, filepath.FromSlash(other)), "original bytes\n", false); err != nil {
		t.Fatal(err)
	}
	before, digest, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	err = board.WithTransactionContext(t.Context(), root, board.LockScope{ExclusiveBoard: true, ReadOnly: true}, func(*board.Transaction) error {
		after, actual, err := snapshotTree(root)
		if err != nil {
			return err
		}
		if actual != digest || !slices.Equal(after, before) || !slices.Contains(after, ".kander/locks/board.lock") {
			t.Fatalf("locked snapshot lost a file or changed its digest: %v", after)
		}
		for rel, expected := range map[string]string{".kander/locks/board.lock": "", other: "original bytes\n"} {
			data, err := readImportFile(root, rel)
			if err != nil || string(data) != expected {
				t.Fatalf("copy %s while locked: %q %v", rel, data, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
