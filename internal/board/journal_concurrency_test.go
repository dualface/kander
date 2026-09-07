package board

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func TestJournalProcessChild(t *testing.T) {
	root := os.Getenv("KANDER_TEST_JOURNAL_ROOT")
	if root == "" {
		return
	}
	mode := os.Getenv("KANDER_TEST_JOURNAL_MODE")
	id := os.Getenv("KANDER_TEST_JOURNAL_TASK")
	if mode == "reader" {
		fmt.Println("ready")
		if _, err := ReadSnapshot(root, id); err != nil {
			t.Fatal(err)
		}
	} else {
		locks, err := acquire(root, LockScope{Tasks: []string{id}})
		if err != nil {
			t.Fatal(err)
		}
		defer locks.close()
		records, err := operationRecords(root)
		if err != nil {
			t.Fatal(err)
		}
		var prepared *OperationRecord
		for i := range records {
			if records[i].ID == "overlap" {
				prepared = &records[i]
				break
			}
		}
		if prepared == nil {
			t.Fatal("missing prepared record")
		}
		fmt.Println("ready")
		if err = applyRecord(root, control(root, "operations", prepared.ID+".json"), prepared); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("complete")
}

func journalChild(t *testing.T, root, mode, id string) (*exec.Cmd, <-chan string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJournalProcessChild$")
	cmd.Env = append(os.Environ(), "KANDER_TEST_JOURNAL_ROOT="+root, "KANDER_TEST_JOURNAL_MODE="+mode, "KANDER_TEST_JOURNAL_TASK="+id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	messages := make(chan string, 4)
	go func() {
		defer close(messages)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			messages <- scanner.Text()
		}
	}()
	select {
	case message := <-messages:
		if message != "ready" {
			t.Fatalf("unexpected child output %q", message)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not reach synchronization point")
	}
	return cmd, messages
}
func journalChildBlocked(t *testing.T, messages <-chan string) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf("journal overlap was not excluded: %q", message)
	case <-time.After(150 * time.Millisecond):
	}
}
func journalChildComplete(t *testing.T, cmd *exec.Cmd, messages <-chan string) {
	t.Helper()
	select {
	case message := <-messages:
		if message != "complete" {
			t.Fatalf("child failed: %q", message)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child remained blocked")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalReadSerializesWithCommittedReplacement(t *testing.T) {
	root := tempBoard(t)
	a := transactionCard(t, root, "journal-writer", false)
	b := transactionCard(t, root, "journal-reader", false)
	path := control(root, "operations", "overlap.json")
	before := a.Text
	after := a.Text + "\nwriter record\n"
	record := OperationRecord{Schema: 1, ID: "overlap", Phase: "prepared", Revisions: map[string]uint64{a.Entry.TaskID: a.Revision + 1}, Files: []FileChange{{Path: filepath.Join("backlog", a.Entry.TaskID, "spec.md"), Before: &before, After: after}}}
	// Pause publication after data/version persistence, just before committed.
	if err := writeOperation(root, path, record, false); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteTextAtomic(root, a.Entry.Document, after, true); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(root, control(root, "versions", a.Entry.TaskID+".json"), versionRecord{Revision: a.Revision + 1, OperationID: record.ID}, true); err != nil {
		t.Fatal(err)
	}
	locks, err := acquire(root, LockScope{Tasks: []string{b.Entry.TaskID}, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	var cmd *exec.Cmd
	var messages <-chan string
	err = withJournalLock(root, true, func() error {
		// On Windows this read handle does not share DELETE. A journal writer
		// must wait until it closes, instead of failing atomic replacement.
		handle, err := fs.OpenRegularFileIfExists(root, path)
		if err != nil {
			return err
		}
		if handle == nil {
			return fmt.Errorf("missing journal")
		}
		defer handle.Close()
		cmd, messages = journalChild(t, root, "writer", a.Entry.TaskID)
		journalChildBlocked(t, messages)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	journalChildComplete(t, cmd, messages)
	records, err := operationRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Phase != "committed" {
			t.Fatalf("left prepared record %s", record.ID)
		}
	}
	current, err := ReadSnapshot(root, a.Entry.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != after || current.Revision != a.Revision+1 {
		t.Fatalf("lost committed update: %+v", current)
	}

}

func TestJournalScanWaitsForTemporaryPublicationHandle(t *testing.T) {
	root := tempBoard(t)
	a := transactionCard(t, root, "temporary-writer", false)
	b := transactionCard(t, root, "temporary-reader", false)
	locks, err := acquire(root, LockScope{Tasks: []string{a.Entry.TaskID}})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	var cmd *exec.Cmd
	var messages <-chan string
	err = withJournalLock(root, false, func() error {
		closeTemporary := holdJournalTemporary(t, root)
		defer closeTemporary()
		cmd, messages = journalChild(t, root, "reader", b.Entry.TaskID)
		journalChildBlocked(t, messages)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	journalChildComplete(t, cmd, messages)
	records, err := operationRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Phase != "committed" {
			t.Fatalf("left prepared record %s", record.ID)
		}
	}
}
