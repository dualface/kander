package board

import (
	"errors"
	"os"
	"testing"
)

// The pre-check of a liveness slot finds a prepared record of its group, ignores
// one of another group, and never lists the committed partition, which can
// hold many records: even a name there that a full journal read refuses does
// not reach it.
func TestRequireNoPendingForReadsOnlyUncommittedRecords(t *testing.T) {
	const group, other = "00000000-pending-for-group", "00000000-other-group"
	root := tempBoard(t)
	stop := errors.New("injected interruption")
	err := withTransactionCheckpoint(t.Context(), root, LockScope{Groups: []string{group}}, func(stage string) error {
		if stage == "prepared" {
			return stop
		}
		return nil
	}, func(tx *Transaction) error { return tx.PutGroup(group, "slot.json", "{}\n") })
	if !errors.Is(err, stop) {
		t.Fatalf("checkpoint did not interrupt: %v", err)
	}
	var rejection *Error
	if err := RequireNoPendingFor(root, group); !errors.As(err, &rejection) || rejection.Code != "board.transaction_pending" {
		t.Fatalf("a prepared record of the group: %v", err)
	}
	if err := RequireNoPendingFor(root, other); err != nil {
		t.Fatalf("a prepared record of another group: %v", err)
	}

	committed := control(root, "operations", "committed")
	if err := os.MkdirAll(committed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(committed+"/not-a-record", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journalFiles(root); err == nil {
		t.Fatal("the full journal listing must refuse the name, or this test proves nothing")
	}
	if err := RequireNoPendingFor(root, other); err != nil {
		t.Fatalf("the committed partition must not be listed: %v", err)
	}
	if err := RequireNoPendingFor(root, group); !errors.As(err, &rejection) || rejection.Code != "board.transaction_pending" {
		t.Fatalf("the prepared record must still be found: %v", err)
	}
}
