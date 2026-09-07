package board

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func TestCoordinatorCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_COORDINATOR_CRASH_ROOT")
	if root == "" {
		return
	}
	stage := os.Getenv("KANDER_COORDINATOR_CRASH_STAGE")
	scope := LockScope{Groups: []string{coordinatorGroup}}
	locks, e := acquire(root, scope)
	if e != nil {
		t.Fatal(e)
	}
	defer locks.close()
	tx := &Transaction{root: root, scope: scope, record: OperationRecord{Schema: 1, ID: "coordinator-crash", Phase: "prepared", Groups: scope.Groups, Revisions: map[string]uint64{}}}
	c, _, e := readCheckpoint(tx, coordinatorGroup)
	if e != nil {
		t.Fatal(e)
	}
	c.Revision++
	c.Authority.Epoch++
	c.Authority.Owner = "successor"
	c.Authority.Token = "successor-token"
	if e = putCheckpoint(tx, &c); e != nil {
		t.Fatal(e)
	}
	if e = validateRecord(root, &tx.record); e != nil {
		t.Fatal(e)
	}
	if stage == "before-intent" {
		crashBoundary()
	}
	path := control(root, "operations", tx.record.ID+".json")
	if e = writeOperation(root, path, tx.record, false); e != nil {
		t.Fatal(e)
	}
	if stage == "prepared" {
		crashBoundary()
	}
	for _, dir := range tx.record.Directories {
		if e = fs.CreatePrivateDirectory(root, filepath.Join(root, dir)); e != nil {
			t.Fatal(e)
		}
	}
	for i, f := range tx.record.Files {
		if e = fs.WriteTextAtomic(root, filepath.Join(root, f.Path), f.After, f.Before != nil); e != nil {
			t.Fatal(e)
		}
		if i == 0 && stage == "history" {
			crashBoundary()
		}
	}
	if stage == "checkpoint" {
		crashBoundary()
	}
	if e = applyRecord(root, path, &tx.record); e != nil {
		t.Fatal(e)
	}
	crashBoundary()
}

func TestCoordinatorKillRestartPreservesCommittedEpoch(t *testing.T) {
	for _, stage := range []string{"before-intent", "prepared", "history", "checkpoint", "committed"} {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			s := coordinatorCard(t, root, "kill")
			before := coordinatorClaim(t, root, s.Entry.TaskID)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_COORDINATOR_CRASH_ROOT="+root, "KANDER_COORDINATOR_CRASH_STAGE="+stage)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			cmd.Stderr = os.Stderr
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
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
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			if e = cmd.Wait(); e == nil {
				t.Fatal("child not killed")
			}
			_, readErr := ReadCoordinatorCheckpoint(root, coordinatorGroup)
			if stage != "before-intent" && stage != "committed" && SnapshotReadStatus(readErr) != "recoverable" {
				t.Fatal("partial checkpoint exposed", readErr)
			}
			if _, _, _, _, e = InitBoardWithOptions("", InitOptions{Maintenance: true}); e != nil {
				t.Fatal(e)
			}
			after, e := ReadCoordinatorCheckpoint(root, coordinatorGroup)
			if e != nil {
				t.Fatal(e)
			}
			if stage == "before-intent" {
				if !reflect.DeepEqual(before, after) {
					t.Fatal("unprepared write visible")
				}
				return
			}
			if after.Revision != before.Revision+1 || after.Authority.Epoch != before.Authority.Epoch+1 || !reflect.DeepEqual(after.Members, before.Members) {
				t.Fatal("lost committed epoch or member facts")
			}
			if _, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, before), nil); e == nil {
				t.Fatal("old epoch wrote after recovery")
			}
			records, e := operationRecords(root)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, _, _, e = InitBoardWithOptions("", InitOptions{Maintenance: true}); e != nil {
				t.Fatal(e)
			}
			again, e := ReadCoordinatorCheckpoint(root, coordinatorGroup)
			if e != nil || !reflect.DeepEqual(after, again) {
				t.Fatal("second recovery rewrote checkpoint", e)
			}
			repeated, e := operationRecords(root)
			if e != nil || len(records) != len(repeated) {
				t.Fatal("duplicate recovery transaction", e)
			}
		})
	}
}
