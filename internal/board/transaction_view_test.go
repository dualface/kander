package board

import (
	"path/filepath"
	"testing"
)

func TestTransactionReadOnlyViewKeepsScopeAndCannotPublish(t *testing.T) {
	root := tempBoard(t)
	first, err := NewTask(root, "chore", "read-only-view", "Inspect a transaction without write authority", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewTask(root, "chore", "outside-read-scope", "Keep undeclared attachments private to their scope", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id, outside := filepath.Base(first), filepath.Base(other)
	before, err := ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	group := "20261112-read-only-group"
	err = WithTransaction(root, LockScope{Tasks: []string{id}, Groups: []string{group}, ExclusiveBoard: true}, func(tx *Transaction) error {
		reader := tx.ReadOnly()
		if err := reader.Put(id, "spec.md", "forbidden"); err == nil {
			t.Fatal("read-only view published a card")
		}
		if err := reader.PutGroup(group, "forbidden.json", "{}"); err == nil {
			t.Fatal("read-only view published control state")
		}
		if _, err := reader.Snapshot(outside); err == nil {
			t.Fatal("read-only view expanded the declared attachment scope")
		}
		view, err := reader.CommittedView()
		if err != nil {
			return err
		}
		document, err := view.Document(id)
		if err != nil {
			return err
		}
		if document != before.Text || view.Entries[id].Version != nil {
			t.Fatal("committed view exposed a mutable cursor or changed text")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := ReadSnapshot(root, id)
	if err != nil || after.Text != before.Text || after.Revision != before.Revision {
		t.Fatal("read-only transaction changed card state")
	}
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		if _, err := tx.CommittedView(); err == nil {
			t.Fatal("full view bypassed the required board-exclusive lock")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
