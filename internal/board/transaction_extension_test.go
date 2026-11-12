package board

import (
	"errors"
	"testing"
)

func TestTransactionExtensionGuardAndAtomicRecovery(t *testing.T) {
	const group = "00000000-test-service-group"
	for _, point := range []string{"guard", "after", "prepared", "files", "revision", "committed"} {
		t.Run(point, func(t *testing.T) {
			root := tempBoard(t)
			s := transactionCard(t, root, "service-tx", false)
			stop := errors.New("interrupted")
			extension := TransactionExtension{Groups: []string{group}, Before: func(tx *Transaction) error {
				if _, err := tx.Expect(s.Entry.TaskID, "backlog", s.Revision); err != nil {
					return err
				}
				if point == "guard" {
					return stop
				}
				return nil
			}, After: func(tx *Transaction) error {
				staged, err := tx.StagedSnapshot(s.Entry.TaskID)
				if err != nil {
					return err
				}
				if staged.Text != s.Text+"\nchange\n" || staged.Revision != s.Revision+1 {
					t.Fatal("receipt did not see the staged result")
				}
				if point == "after" {
					return stop
				}
				return tx.PutGroup(group, "receipt.json", `{"committed":true}`)
			}}
			err := withTransactionCheckpoint(t.Context(), root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(stage string) error {
				if stage == point {
					return stop
				}
				return nil
			}, func(tx *Transaction) error { return tx.Put(s.Entry.TaskID, "spec.md", s.Text+"\nchange\n") }, extension)
			if point == "guard" || point == "after" {
				if !errors.Is(err, stop) {
					t.Fatalf("expected validation failure: %v", err)
				}
				got := transactionSnapshot(t, root, s.Entry.TaskID)
				if got.Text != s.Text || got.Revision != s.Revision {
					t.Fatal("failed preparation published a change")
				}
				return
			}
			// Every publication checkpoint leaves recoverable evidence.
			if !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if err := RecoverTransactions(root); err != nil {
				t.Fatal(err)
			}
			got := transactionSnapshot(t, root, s.Entry.TaskID)
			if got.Text != s.Text+"\nchange\n" || got.Revision != s.Revision+1 {
				t.Fatal("recovery did not publish the card")
			}
			err = WithTransaction(root, LockScope{Groups: []string{group}, ReadOnly: true}, func(tx *Transaction) error {
				data, found, err := tx.ReadGroup(group, "receipt.json")
				if err != nil {
					return err
				}
				if !found || string(data) != `{"committed":true}` {
					t.Fatal("recovery lost the receipt")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
