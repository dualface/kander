package board

import (
	"errors"
	"testing"
)

func TestAtomicReviewRunPublicationRollsBackEveryMemberOnReceiptFailure(t *testing.T) {
	root := tempBoard(t)
	ids := []string{archiveCard(t, root, "atomic-first"), archiveCard(t, root, "atomic-second")}
	run := finalizedRun(t, root, archiveInput(ids, "atomic-run", "PMQA"))
	before := make(map[string]Snapshot)
	for _, id := range ids {
		before[id] = transactionSnapshot(t, root, id)
	}
	rejected := errors.New("injected service receipt failure")
	_, err := PublishReviewRunAtomic(root, run.RunID, TransactionExtension{After: func(*Transaction) error { return rejected }})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	for _, id := range ids {
		after := transactionSnapshot(t, root, id)
		if after.Revision != before[id].Revision || after.Text != before[id].Text {
			t.Fatal("partial member publication escaped the transaction")
		}
	}
	published, err := PublishReviewRunAtomic(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	operation := ""
	for _, id := range ids {
		after := transactionSnapshot(t, root, id)
		if after.Revision != before[id].Revision+1 || !published.Published[id] {
			t.Fatal("member missing atomic publication")
		}
		if operation != "" && after.OperationID != operation {
			t.Fatal("member copies used separate redo transactions")
		}
		operation = after.OperationID
	}
	if err := ReviewPublicationComplete(root, run.RunID); err != nil {
		t.Fatal(err)
	}
}
