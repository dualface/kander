package board

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCreationAndInitialReceiptRecoverAsOneOperation(t *testing.T) {
	const group = "00000000-create-receipt-group"
	for _, point := range []string{"after", "prepared", "files", "revision", "committed"} {
		t.Run(point, func(t *testing.T) {
			root := tempBoard(t)
			id := todayPrefix() + "-create-recovery-task"
			stop := errors.New("injected creation interruption")
			var initial Snapshot
			extension := TransactionExtension{Groups: []string{group}, After: func(tx *Transaction) error {
				var ok bool
				initial, ok = tx.OperationResult().(Snapshot)
				if !ok || initial.Entry.TaskID != id || initial.Revision != 1 || initial.OperationID == "" {
					t.Fatalf("initial snapshot missing: %+v", initial)
				}
				data, err := json.Marshal(initial)
				if err != nil {
					return err
				}
				if point == "after" {
					return stop
				}
				return tx.PutGroup(group, "receipt.json", string(data))
			}}
			err := withTransactionCheckpoint(t.Context(), root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(stage string) error {
				if stage == point {
					return stop
				}
				return nil
			}, func(tx *Transaction) error {
				_, err := newTask(tx, root, "chore", "create-recovery", "  Original title  ", "en", true)
				return err
			}, extension)
			if !errors.Is(err, stop) {
				t.Fatalf("checkpoint did not interrupt: %v", err)
			}
			if point == "prepared" {
				// A retry before recovery must not publish a replacement receipt.
				_, err := NewTaskWithTransaction(root, "chore", "create-recovery", "Other title", "en", false, TransactionExtension{Groups: []string{group}})
				var rejection *Error
				if !errors.As(err, &rejection) || rejection.Code != "board.transaction_pending" {
					t.Fatalf("pending creation accepted another producer: %v", err)
				}
			}
			if err := RecoverTransactions(root); err != nil {
				t.Fatal(err)
			}
			if point == "after" {
				view, err := LoadBoard(root)
				if err != nil || len(view.Entries) != 0 {
					t.Fatalf("failed preparation published a card: %v", err)
				}
				return
			}
			got := transactionSnapshot(t, root, id)
			// Version is a local file handle identity, absent before publication
			// and excluded from serialized snapshots and service receipts.
			got.Entry.Version = nil
			if !reflect.DeepEqual(got, initial) {
				t.Fatalf("creation recovery changed initial result: got=%+v initial=%+v", got, initial)
			}
			err = WithTransaction(root, LockScope{Groups: []string{group}, ReadOnly: true}, func(tx *Transaction) error {
				raw, found, err := tx.ReadGroup(group, "receipt.json")
				if err != nil {
					return err
				}
				var saved Snapshot
				if !found || json.Unmarshal(raw, &saved) != nil || !reflect.DeepEqual(saved, initial) {
					t.Fatal("creation receipt did not recover with its card")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Entry.Document != filepath.Join(got.Entry.Path, "spec.md") {
				t.Fatal("creation did not retain directory-card identity")
			}
		})
	}
}
