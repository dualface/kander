package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func contextTask(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for _, state := range States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewTask(root, "chore", "context-snapshot", "Context", "en", false); err != nil {
		t.Fatal(err)
	}
	scanned, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for id := range scanned.Entries {
		return root, id
	}
	t.Fatal("no task")
	return "", ""
}

func TestSnapshotContextContentionReleasesLocks(t *testing.T) {
	for _, name := range []string{"board", "task", "journal"} {
		t.Run(name, func(t *testing.T) {
			root, id := contextTask(t)
			file := name + ".lock"
			if name == "task" {
				file = id + ".lock"
			}
			var holder lockSet
			if err := holder.take(root, control(root, "locks", file), false); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := holder.close(); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := ScanTargetsContext(ctx, root, []string{id})
			if !errors.Is(err, context.DeadlineExceeded) || SnapshotReadStatus(err) != "maintenance" {
				t.Fatalf("err=%v", err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("unbounded lock wait")
			}
			if err := holder.close(); err != nil {
				t.Fatal(err)
			}
			ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
			defer cancel2()
			if _, err := ScanContext(ctx2, root); err != nil {
				t.Fatalf("read locks leaked: %v", err)
			}
			// A subsequent writer must not be blocked by a timed-out reader.
			completed := make(chan error, 1)
			go func() {
				s, e := ReadSnapshot(root, id)
				if e == nil {
					e = UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: s.Text + "\nrecord\n", ExpectedRevision: s.Revision})
				}
				completed <- e
			}()
			select {
			case err := <-completed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("writer blocked after canceled read")
			}
		})
	}
}

func TestSnapshotPendingFactsFailWithoutRecovery(t *testing.T) {
	for _, creation := range []bool{false, true} {
		t.Run(map[bool]string{false: "document", true: "creation"}[creation], func(t *testing.T) {
			root, id := contextTask(t)
			s, err := ReadSnapshot(root, id)
			if err != nil {
				t.Fatal(err)
			}
			operation, err := operationID()
			if err != nil {
				t.Fatal(err)
			}
			record := OperationRecord{Schema: 1, ID: operation, Phase: "prepared", Revisions: map[string]uint64{id: s.Revision + 1}}
			before := s.Text
			record.Files = []FileChange{{Path: filepath.Join(s.Entry.State, id, "spec.md"), Before: &before, After: before + "\nnew text\n"}}
			if creation {
				newID := "20260908-pending-create-task"
				record.Revisions = map[string]uint64{newID: 1}
				record.Files = nil
				record.Entries = []EntryChange{{To: filepath.Join("backlog", newID), Kind: "large", Text: before}}
			}
			if err = writeOperation(root, control(root, "operations", operation+".json"), record, false); err != nil {
				t.Fatal(err)
			}
			for _, all := range []bool{false, true} {
				var readErr error
				if all {
					_, readErr = ScanContext(context.Background(), root)
				} else {
					_, readErr = ScanTargetsContext(context.Background(), root, []string{id})
				}
				if SnapshotReadStatus(readErr) != "recoverable" {
					t.Fatalf("pending lost: %v", readErr)
				}
			}
			raw, err := os.ReadFile(s.Entry.Document)
			if err != nil || string(raw) != before {
				t.Fatalf("reader repaired: %s %v", raw, err)
			}
			records, err := operationRecords(root)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range records {
				if r.ID == operation {
					found = r.Phase == "prepared"
				}
			}
			if !found {
				t.Fatal("pending record changed")
			}
		})
	}
}

func TestSnapshotRevisionSurvivesMutationCursor(t *testing.T) {
	root, id := contextTask(t)
	captured, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := captured.Revision(id)
	if err != nil {
		t.Fatal(err)
	}
	text, err := captured.Document(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteManagedDocument(root, captured.Entries[id], text+"\nnew\n"); err != nil {
		t.Fatal(err)
	}
	previous, err := captured.Revision(id)
	if err != nil || previous != revision {
		t.Fatalf("captured version changed: %d %v", previous, err)
	}
	fresh, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := fresh.Revision(id)
	if err != nil || latest != revision+1 {
		t.Fatalf("fresh version %d %v", latest, err)
	}
}

func TestMembershipDependencyRejectsUnknownOwnership(t *testing.T) {
	root, id := contextTask(t)
	s, err := ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: 20260908-source-group", 1)
	text = strings.Replace(text, "## DISCUSSION", "## DISCUSSION\n\n```text\nPREREQUISITES: 20260908-external-group\n```", 1)
	if err = UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = NewTask(root, "chore", "membership-target", "Target", "en", false); err != nil {
		t.Fatal(err)
	}
	scanned, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var target string
	for candidate := range scanned.Entries {
		if candidate != id {
			target = candidate
		}
	}
	s, err = ReadSnapshot(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if err = UpdateDocument(root, target, UpdateOptions{Document: "spec.md", Text: strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: 20260908-external-group", 1), ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	scanned, err = Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := TaskDependenciesOf(scanned.Entries[id], scanned, nil)
	if err != nil || len(deps.ExpandedTaskIDs) != 1 || deps.ExpandedTaskIDs[0] != target {
		t.Fatalf("deps %+v %v", deps, err)
	}
	if err = os.WriteFile(s.Entry.Document, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	scanned, err = Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = TaskDependenciesOf(scanned.Entries[id], scanned, map[string]string{id: text}); err == nil {
		t.Fatal("partial dependency group accepted")
	}
	code, _, _, err := CheckBoard(root, []string{id}, false)
	if err == nil && code == 0 {
		t.Fatal("check released unknown membership")
	}
}
