package liveness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

func TestSubscribeUnavailableCommittedFacts(t *testing.T) {
	for _, mode := range []string{"prepared", "maintenance", "corrupt-journal", "reparse"} {
		t.Run(mode, func(t *testing.T) {
			root, opts, id := factsMember(t)
			snapshot, err := board.ReadSnapshot(root, id)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "prepared":
				operation := "abcdefabcdefabcdefabcdefabcdefab"
				text := snapshot.Text
				record := board.OperationRecord{Schema: 1, ID: operation, Phase: "prepared", Revisions: map[string]uint64{id: snapshot.Revision + 1}, Files: []board.FileChange{{Path: filepath.Join("review", id, "spec.md"), Before: &text, After: text + "\nnot committed\n"}}}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, ".kander", "operations", operation+".json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			case "maintenance":
				file, err := fs.OpenLockFile(root, filepath.Join(root, ".kander", "locks", "board.lock"))
				if err != nil {
					t.Fatal(err)
				}
				lock, err := fs.LockExclusive(file)
				if err != nil {
					file.Close()
					t.Fatal(err)
				}
				defer file.Close()
				defer lock.Unlock()
			case "corrupt-journal":
				if err = os.WriteFile(filepath.Join(root, ".kander", "operations", "bad.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "reparse":
				if err = os.Symlink(snapshot.Entry.Path, filepath.Join(root, "todo", id)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			start := time.Now()
			events, err := factsEvents(t, root, opts, nil)
			if err == nil || len(events) != 1 {
				t.Fatalf("normal facts emitted: %+v %v", events, err)
			}
			expected := "invalid"
			if mode == "prepared" {
				expected = "recoverable"
			}
			if mode == "maintenance" {
				expected = "maintenance"
			}
			if events[0].ReadStatus != expected || events[0].MembershipComplete || !events[0].ReconciliationRequired {
				t.Fatalf("unsafe failure: %+v", events[0])
			}
			if mode == "maintenance" && time.Since(start) > subscriptionReadTimeout+time.Second {
				t.Fatal("unbounded maintenance wait")
			}
			raw, err := os.ReadFile(snapshot.Entry.Document)
			if err != nil || string(raw) != snapshot.Text {
				t.Fatal("subscriber repaired board")
			}
		})
	}
}
