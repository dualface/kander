package board

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func TestJournalPartitionCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_TEST_PARTITION_ROOT")
	if root == "" {
		return
	}
	stage := os.Getenv("KANDER_TEST_PARTITION_STAGE")
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	checkpoint := func(at string) error {
		if at == stage {
			crashBoundary()
		}
		return nil
	}
	if stage == "journal-partitioned" {
		records, err := operationRecords(root)
		if err != nil {
			t.Fatal(err)
		}
		err = partitionJournalWithCheckpoint(root, records, InitOptions{}, checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	file := journalFile{"crash-partition.json", "pending"}
	r, err := readJournalRecord(root, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyRecordWithCheckpoint(root, file.path(root), &r, checkpoint); err != nil {
		t.Fatal(err)
	}
}

func TestJournalPartitionKillRestart(t *testing.T) {
	for _, stage := range []string{"prepared", "journal-committed-phase", "journal-pruned", "journal-partitioned"} {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			s := transactionCard(t, root, "partition-crash", false)
			before := s.Text
			r := OperationRecord{Schema: 1, ID: "crash-partition", Phase: "prepared", Revisions: map[string]uint64{s.Entry.TaskID: s.Revision + 1}, Files: []FileChange{{Path: "backlog/" + s.Entry.TaskID + "/spec.md", Before: &before, After: before + "\ncrash recovery\n"}}}
			if err := writeOperation(root, control(root, "operations", "pending", r.ID+".json"), r, false); err != nil {
				t.Fatal(err)
			}
			partition := "committed"
			if stage == "journal-partitioned" {
				partition = ""
			}
			journalHistory(t, root, partition, committedJournalRetention+5)
			// An unrelated prepared record must survive even a kill during pruning.
			other := OperationRecord{Schema: 1, ID: "other-pending", Phase: "prepared", Revisions: map[string]uint64{"20260908-other-task": 1}}
			if err := writeOperation(root, control(root, "operations", "pending", other.ID+".json"), other, false); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestJournalPartitionCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_TEST_PARTITION_ROOT="+root, "KANDER_TEST_PARTITION_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			reached := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				reached <- scanner.Scan() && scanner.Text() == "crash-boundary"
			}()
			select {
			case ok := <-reached:
				if !ok {
					t.Fatal("child failed before checkpoint")
				}
			case <-time.After(15 * time.Second):
				t.Fatal("checkpoint timeout")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("child was not killed")
			}
			if _, err := fs.ReadRegularFile(root, control(root, "operations", "pending", other.ID+".json")); err != nil {
				t.Fatal("pruning lost prepared transaction", err)
			}
			if stage == "prepared" || stage == "journal-partitioned" {
				if _, err := ReadSnapshot(root, s.Entry.TaskID); err == nil {
					t.Fatal("pending update became visible")
				}
			}
			if _, err := MigrateCards(root, InitOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := RecoverTransactions(root); err != nil {
				t.Fatal("repeat recovery", err)
			}
			current := transactionSnapshot(t, root, s.Entry.TaskID)
			if current.Revision != s.Revision+1 || current.Text != r.Files[0].After || current.OperationID != r.ID {
				t.Fatalf("incorrect recovery: %+v", current)
			}
			pending, err := fs.ListDirectory(root, control(root, "operations", "pending"))
			if err != nil || len(pending) != 0 {
				t.Fatalf("pending after recovery: %v %v", pending, err)
			}
			committed, err := fs.ListDirectory(root, control(root, "operations", "committed"))
			if err != nil || len(committed) != committedJournalRetention {
				t.Fatalf("pruning did not resume: %d %v", len(committed), err)
			}
		})
	}
}

func TestJournalCleanupSerializesWithProcessReader(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "cleanup-reader", false)
	journalHistory(t, root, "committed", committedJournalRetention+5)
	var cmd *exec.Cmd
	var messages <-chan string
	err := withJournalLock(root, false, func() error {
		cmd, messages = journalChild(t, root, "reader", s.Entry.TaskID)
		journalChildBlocked(t, messages)
		return pruneCommitted(root, func(string) error { return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	journalChildComplete(t, cmd, messages)
	if _, err := os.Stat(control(root, "operations", "committed", fmt.Sprintf("history-%04d.json", 0))); !os.IsNotExist(err) {
		t.Fatal("old record not pruned", err)
	}
}
