package liveness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestGroupExpansionKeepsCommittedSnapshot(t *testing.T) {
	root := tempBoard(t)
	group := "20260907-member-race-group"
	var ids []string
	for _, slug := range []string{"member-a", "member-b"} {
		if _, err := board.NewTask(root, "chore", slug, "member", "en", false); err != nil {
			t.Fatal(err)
		}
		id := todayID(slug)
		ids = append(ids, id)
		s, err := board.ReadSnapshot(root, id)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		if err = board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	scanned, err := board.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := board.ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, ids[0], board.UpdateOptions{Document: "spec.md", Text: fresh.Text + "\nupdated execution record\n", ExpectedRevision: fresh.Revision}); err != nil {
		t.Fatal(err)
	}
	// The update occurs after Scan and before the consumer reads its entries.
	membership := scanned.GroupMembership()
	members, err := membership.Groups, membership.Err()
	if err != nil || !reflect.DeepEqual(members[group], ids) {
		t.Fatalf("partial expansion returned: %+v, %v", members, err)
	}
	scanned, err = board.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	membership = scanned.GroupMembership()
	members, err = membership.Groups, membership.Err()
	if err != nil || !reflect.DeepEqual(members[group], ids) {
		t.Fatalf("fresh expansion: %+v, %v", members, err)
	}
}
