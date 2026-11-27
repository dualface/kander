package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkBackup puts a symlink to a file outside the board in place of the
// backup of id and returns the backup path and the link target. A platform
// that cannot create the link skips the test.
func linkBackup(t *testing.T, root, id string) (backup, target string) {
	t.Helper()
	target = filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensurePruneDir(root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	backup = compactionBackupPath(root, id)
	// Windows needs SeCreateSymbolicLinkPrivilege or developer mode.
	if err := os.Symlink(target, backup); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	return backup, target
}

// orphanBackup writes the backup of a run that never existed: the state a
// round interrupted after it removed the run and before its backup leaves.
func orphanBackup(t *testing.T, root, id string) string {
	t.Helper()
	if err := ensurePruneDir(root, "compaction-backups"); err != nil {
		t.Fatal(err)
	}
	backup := compactionBackupPath(root, id)
	if err := os.WriteFile(backup, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return backup
}

func requireLinkUntouched(t *testing.T, backup, target string) {
	t.Helper()
	if link, err := os.Readlink(backup); err != nil || link != target {
		t.Fatalf("the symlink backup changed: %q %v", link, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "outside\n" {
		t.Fatalf("the link target changed: %q %v", data, err)
	}
}

func requireBackupGone(t *testing.T, backup string) {
	t.Helper()
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the orphan backup %s is still there: %v", backup, err)
	}
}

// An orphan backup beside a symlink backup of a run that still exists goes in
// the same round. Only the linked run stays, the link and its target are left
// alone, an unrelated batch still goes, and once the link is gone the next
// round removes the linked run as well.
func TestArchivePruneSettlesAnOrphanBesideASymlinkBackup(t *testing.T) {
	f := &backupFixture{root: tempBoard(t)}
	a := f.compactedRun(t, "backup-orphan-link-a")
	c := f.settledRun(t, "backup-orphan-link-c")
	f.archive(t)
	linked, target := linkBackup(t, f.root, a.RunID)
	orphan := orphanBackup(t, f.root, "backup-orphan-link-b-run")

	_, err := PruneArchived(f.root)
	requireBackupGone(t, orphan)
	requireRunPruned(t, f.root, a, false)
	requireReceipt(t, f.root, a)
	requireRunPruned(t, f.root, c, true)
	if err == nil || !strings.Contains(err.Error(), linked) {
		t.Fatalf("want the error of %s, got %v", linked, err)
	}
	requireLinkUntouched(t, linked, target)

	if err := os.Remove(linked); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneArchived(f.root); err != nil {
		t.Fatalf("prune after removing the link: %v", err)
	}
	requireRunPruned(t, f.root, a, true)
}

// An orphan backup beside a symlink backup of a run that never existed goes
// in the same round; the link is reported and left alone.
func TestArchivePruneSettlesAnOrphanBesideTheSymlinkBackupOfAGoneRun(t *testing.T) {
	root := tempBoard(t)
	linked, target := linkBackup(t, root, "backup-gone-link-a-run")
	orphan := orphanBackup(t, root, "backup-gone-link-b-run")

	_, err := PruneArchived(root)
	requireBackupGone(t, orphan)
	if err == nil || !strings.Contains(err.Error(), linked) {
		t.Fatalf("want the error of %s, got %v", linked, err)
	}
	requireLinkUntouched(t, linked, target)
}
