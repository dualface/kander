package board

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func journalHistory(t *testing.T, root, partition string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("history-%04d", i)
		r := OperationRecord{Schema: 1, ID: name, Phase: "committed"}
		path := control(root, "operations", partition, name+".json")
		if err := writeJSON(root, path, r, false); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(1000+int64(i), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJournalPartitionPublishAndReadFastPath(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "journal-fast", false)
	files, err := fs.ListDirectory(root, control(root, "operations", "pending"))
	if err != nil || len(files) != 0 {
		t.Fatalf("pending after commit: %v %v", files, err)
	}
	if _, err := fs.ReadRegularFile(root, control(root, "operations", "committed", s.OperationID+".json")); err != nil {
		t.Fatal(err)
	}
	// Invalid JSON and a large sparse record prove that readers do not decode
	// committed images, while full recovery inspection still rejects them.
	for _, name := range []string{"invalid", "huge"} {
		path := control(root, "operations", "committed", name+".json")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("invalid JSON"); err != nil {
			t.Fatal(err)
		}
		if name == "huge" {
			if err := file.Truncate(128 << 20); err != nil {
				t.Fatal(err)
			}
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadSnapshot(root, s.Entry.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanContext(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := pendingContext(context.Background(), root, []string{s.Entry.TaskID}); err != nil {
		t.Fatal(err)
	}
	if _, err := operationRecords(root); err == nil {
		t.Fatal("full journal inspection accepted invalid JSON")
	}
}

func TestJournalRejectsStructuralCorruption(t *testing.T) {
	for _, partition := range []string{"", "pending", "committed"} {
		for _, kind := range []string{"bad-name.json", "not-json", "directory", "hidden", "symlink", "duplicate"} {
			t.Run(partition+"/"+kind, func(t *testing.T) {
				root := tempBoard(t)
				s := transactionCard(t, root, "journal-shape", false)
				path := control(root, "operations", partition, "unexpected.txt")
				switch kind {
				case "bad-name.json":
					path = control(root, "operations", partition, "bad name.json")
				case "hidden":
					path = control(root, "operations", partition, ".hidden")
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(t.TempDir(), path); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				case "duplicate":
					if partition == "committed" {
						path = control(root, "operations", "pending", s.OperationID+".json")
					} else {
						path = control(root, "operations", partition, s.OperationID+".json")
					}
				}
				if kind != "directory" && kind != "symlink" {
					if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := ReadSnapshot(root, s.Entry.TaskID); err == nil {
					t.Fatal("accepted malformed journal structure")
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatal("removed evidence", err)
				}
			})
		}
	}
}

func TestJournalLegacyPendingAndInitHint(t *testing.T) {
	for _, partition := range []string{"", "pending"} {
		t.Run(partition, func(t *testing.T) {
			root := tempBoard(t)
			s := transactionCard(t, root, "journal-prepared", false)
			r := OperationRecord{Schema: 1, ID: "unfinished", Phase: "prepared", Revisions: map[string]uint64{s.Entry.TaskID: s.Revision + 1}}
			if err := writeJSON(root, control(root, "operations", partition, r.ID+".json"), r, false); err != nil {
				t.Fatal(err)
			}
			_, _, stderr := capture(t, func() int {
				for _, read := range []func() error{
					func() error { _, err := ReadSnapshot(root, s.Entry.TaskID); return err },
					func() error { _, err := Scan(root); return err },
				} {
					err := read()
					if err == nil || !strings.Contains(err.Error(), r.ID) || !strings.Contains(err.Error(), "kander init") {
						t.Errorf("missing pending identity/hint: %v", err)
					}
				}
				return 0
			})
			if partition == "" && !strings.Contains(stderr, "kander init") {
				t.Fatal("missing legacy layout hint")
			}
		})
	}
}

func TestJournalPartitionPreservesBytesAndResumes(t *testing.T) {
	root := tempBoard(t)
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	originals := map[string]string{
		"old-a.json": "{\"schema\":1,\"operation_id\":\"old-a\",\"phase\":\"committed\"}\n\n",
		"old-b.json": "{ \"schema\":1, \"operation_id\":\"old-b\", \"phase\":\"prepared\" }\n",
	}
	for name, text := range originals {
		if err := os.WriteFile(control(root, "operations", name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	records, err := operationRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("interrupted partition")
	err = partitionJournalWithCheckpoint(root, records, InitOptions{}, func(string) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	records, err = operationRecords(root)
	if err != nil || len(records) != 2 {
		t.Fatalf("lost records after partial partition: %v %v", records, err)
	}
	if err := partitionJournal(root, records, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	_, _, stderr := capture(t, func() int {
		if err := partitionJournal(root, records, InitOptions{}); err != nil {
			t.Error(err)
		}
		return 0
	})
	if !strings.Contains(stderr, "0") {
		t.Fatalf("missing zero partition count: %s", stderr)
	}
	for _, record := range records {
		partition := "pending"
		if record.Phase == "committed" {
			partition = "committed"
		}
		data, err := fs.ReadRegularFile(root, control(root, "operations", partition, record.ID+".json"))
		if err != nil || string(data) != originals[record.ID+".json"] {
			t.Fatalf("changed legacy bytes: %q %v", data, err)
		}
	}
}

func TestJournalInitMaintenanceAndRetention(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "journal-maintenance", false)
	if err := fs.Rename(root, s.Entry.Path, filepath.Join(root, "working", s.Entry.TaskID)); err != nil {
		t.Fatal(err)
	}
	journalHistory(t, root, "", committedJournalRetention+5)
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("legacy partition bypassed active writer maintenance")
	}
	if _, err := MigrateCards(root, InitOptions{Maintenance: true}); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ListDirectory(root, control(root, "operations", "committed"))
	if err != nil || len(files) != committedJournalRetention {
		t.Fatalf("retention: %d %v", len(files), err)
	}
	for i := 0; i < 6; i++ {
		if ok, err := fs.RegularFileExists(root, control(root, "operations", "committed", fmt.Sprintf("history-%04d.json", i))); err != nil || ok {
			t.Fatalf("old history survived: %d %v", i, err)
		}
	}
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 0 {
		t.Fatalf("repeat init: %d %v", n, err)
	}
}

func TestJournalRetentionPinsMigrationEvidence(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	path := control(root, "operations", "committed", r.ID+".json")
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	journalHistory(t, root, "committed", committedJournalRetention+5)
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	records, err := operationRecords(root)
	if err != nil || len(records) != committedJournalRetention+1 {
		t.Fatalf("lost pinned migration: %d %v", len(records), err)
	}
	if err := validateMigrationStaging(root, records); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadRegularFile(root, path); err != nil {
		t.Fatal(err)
	}
}

func TestJournalCleanupFailureDoesNotUndoCommit(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "journal-warning", false)
	r := OperationRecord{Schema: 1, ID: "warning-operation", Phase: "prepared", Revisions: map[string]uint64{s.Entry.TaskID: s.Revision + 1}}
	path := control(root, "operations", "pending", r.ID+".json")
	if err := writeOperation(root, path, r, false); err != nil {
		t.Fatal(err)
	}
	_, _, stderr := capture(t, func() int {
		err := applyRecordWithCheckpoint(root, path, &r, func(at string) error {
			if at == "journal-committed-phase" {
				return os.WriteFile(control(root, "migrations", "not-a-directory"), []byte("preserve"), 0600)
			}
			return nil
		})
		if err != nil {
			t.Errorf("cleanup changed publication result: %v", err)
		}
		return 0
	})
	if !strings.Contains(stderr, "not-a-directory") {
		t.Fatalf("cleanup failure not warned: %s", stderr)
	}
	current := transactionSnapshot(t, root, s.Entry.TaskID)
	if current.Revision != s.Revision+1 || current.OperationID != r.ID {
		t.Fatal("committed result lost")
	}
	if _, err := fs.ReadRegularFile(root, control(root, "operations", "committed", r.ID+".json")); err != nil {
		t.Fatal(err)
	}
}
