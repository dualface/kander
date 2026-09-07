package board

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// The child stages through the same intent/receipt producers as the public
// API, then pauses publication at durable boundaries for an actual OS kill.
func TestDispatchCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_DISPATCH_CRASH_ROOT")
	if root == "" {
		return
	}
	id := os.Getenv("KANDER_DISPATCH_CRASH_TASK")
	mode := os.Getenv("KANDER_DISPATCH_CRASH_MODE")
	stage := os.Getenv("KANDER_DISPATCH_CRASH_STAGE")
	scope := LockScope{Tasks: []string{id}, Groups: []string{dispatchRegistry, reviewControlGroup}, ExclusiveBoard: true}
	locks, err := acquire(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	tx := &Transaction{root: root, scope: scope, record: OperationRecord{Schema: 1, ID: "dispatch-crash", Phase: "prepared", Revisions: map[string]uint64{}, Groups: scope.Groups}}
	s, err := tx.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "create" {
		in := dispatchInput(s, "crash-intent")
		in.CreatedAt = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
		in.ConfirmBy = in.CreatedAt.AddDate(10, 0, 0)
		var d Dispatch
		if err = tx.prepareDispatch(in, &d); err != nil {
			t.Fatal(err)
		}
	} else {
		d, err := readDispatch(tx, id, "crash-intent")
		if err != nil {
			t.Fatal(err)
		}
		target := "working"
		o := MoveOptions{Authorization: d.Authorization}
		if mode == "complete" || mode == "done" {
			target = "review"
			o.DeliveryCommit = strings.Repeat("b", 40)
		}
		if mode == "done" {
			target = "done"
			o.Result = "completed"
		}
		if _, err = stageDispatchMove(tx, s, target, o); err != nil {
			t.Fatal(err)
		}
		text, err := moveMetadata(s.Text, s.Entry.State, target, o)
		if err != nil {
			t.Fatal(err)
		}
		if target == "done" {
			text, err = completionMetadata(text)
			if err != nil {
				t.Fatal(err)
			}
		}
		if text != s.Text {
			if err = tx.Put(id, "spec.md", text); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Relocate(id, target); err != nil {
			t.Fatal(err)
		}
	}
	rec := &tx.record
	if err = validateRecord(root, rec); err != nil {
		t.Fatal(err)
	}
	path := control(root, "operations", rec.ID+".json")
	if err = writeOperation(root, path, *rec, false); err != nil {
		t.Fatal(err)
	}
	if stage == "prepared" {
		crashBoundary()
	}
	for _, dir := range rec.Directories {
		if err = fs.CreatePrivateDirectory(root, filepath.Join(root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "directories" {
		crashBoundary()
	}
	for i, f := range rec.Files {
		if err = fs.WriteTextAtomic(root, filepath.Join(root, f.Path), f.After, f.Before != nil); err != nil {
			t.Fatal(err)
		}
		if stage == "first-file" && i == 0 {
			crashBoundary()
		}
	}
	if stage == "files" {
		crashBoundary()
	}
	for _, e := range rec.Entries {
		if err = fs.Rename(root, filepath.Join(root, e.From), filepath.Join(root, e.To)); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "rename" {
		crashBoundary()
	}
	for task, v := range rec.Revisions {
		if err = writeJSON(root, control(root, "versions", task+".json"), versionRecord{Revision: v, OperationID: rec.ID, ContractFrozen: true}, true); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "revision" {
		crashBoundary()
	}
	if err = applyRecord(root, path, rec); err != nil {
		t.Fatal(err)
	}
	crashBoundary()
}

func TestDispatchKillRecovery(t *testing.T) {
	for _, mode := range []string{"create", "accept", "complete", "done"} {
		for _, stage := range []string{"prepared", "directories", "first-file", "files", "rename", "revision", "committed"} {
			t.Run(mode+"/"+stage, func(t *testing.T) {
				root := tempBoard(t)
				s := dispatchCard(t, root, "crash")
				in := dispatchInput(s, "crash-intent")
				in.CreatedAt = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
				in.ConfirmBy = in.CreatedAt.AddDate(10, 0, 0)
				if mode != "create" {
					if mode == "done" {
						bindWrapUpFixture(t, root, &in)
					}
					d := prepareTestDispatch(t, root, in)
					if mode == "complete" || mode == "done" {
						if _, err := dispatchMove(t, root, d, "working"); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := transactionSnapshot(t, root, s.Entry.TaskID)
				cmd := exec.Command(os.Args[0], "-test.run=^TestDispatchCrashChild$")
				cmd.Env = append(os.Environ(), "KANDER_DISPATCH_CRASH_ROOT="+root, "KANDER_DISPATCH_CRASH_TASK="+s.Entry.TaskID, "KANDER_DISPATCH_CRASH_MODE="+mode, "KANDER_DISPATCH_CRASH_STAGE="+stage)
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				cmd.Stderr = os.Stderr
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				}()
				ready := make(chan bool, 1)
				go func() {
					scanner := bufio.NewScanner(stdout)
					ready <- scanner.Scan() && scanner.Text() == "crash-boundary"
				}()
				select {
				case ok := <-ready:
					if !ok {
						t.Fatal("child failed")
					}
				case <-time.After(15 * time.Second):
					t.Fatal("child timeout")
				}
				if err = cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				if err = cmd.Wait(); err == nil {
					t.Fatal("child not killed")
				}
				if stage != "committed" {
					if _, err = ReadSnapshot(root, s.Entry.TaskID); err == nil {
						t.Fatal("partial receipt exposed")
					}
				}
				if _, _, _, _, err = InitBoardWithOptions("", InitOptions{Maintenance: true}); err != nil {
					t.Fatal(err)
				}
				after := transactionSnapshot(t, root, s.Entry.TaskID)
				d, err := ReadDispatch(root, s.Entry.TaskID, "crash-intent")
				if err != nil {
					t.Fatal(err)
				}
				if after.Revision != before.Revision+1 {
					t.Fatalf("revision %d -> %d", before.Revision, after.Revision)
				}
				expected := DispatchPrepared
				state := "review"
				if mode == "accept" {
					expected = DispatchAccepted
					state = "working"
				}
				if mode == "complete" || mode == "done" {
					expected = DispatchCompleted
				}
				if mode == "done" {
					state = "done"
				}
				if d.State != expected || after.Entry.State != state {
					t.Fatalf("state/receipt differ: %s %s", after.Entry.State, d.State)
				}
				in.CreatedAt = time.Time{}
				in.ConfirmBy = time.Time{}
				retried, err := PrepareDispatch(root, in)
				if err != nil {
					t.Fatal(err)
				}
				if !retried.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
					t.Fatal("deadline reset")
				}
				all, err := Scan(root)
				if err != nil || len(all.Entries) != 1 {
					t.Fatalf("duplicate after recovery: %v", err)
				}
				raw, err := os.ReadFile(filepath.Join(after.Entry.Path, dispatchPath(d.Input.ID, "intent")))
				if err != nil {
					t.Fatal(err)
				}
				var original Dispatch
				if json.Unmarshal(raw, &original) != nil || original.Input.ID != d.Input.ID {
					t.Fatal("original lost")
				}
			})
		}
	}
}
