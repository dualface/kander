package server_test

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/server"
)

// The holder appends a correction record through the service under the authorization of an
// accepted sync round, the round that carries a correction after a rebase rewrote a fix commit.
// Without the authorization the bound card refuses the record.
func TestHolderAppendsACorrectionRecordUnderAnAcceptedSyncRound(t *testing.T) {
	h := newReviewedHolder(t, "correction-record")
	root, err := h.f.srv.BoardRoot(h.holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	run, err := board.ReadReviewRun(root, h.evidence.RunID)
	if err != nil {
		t.Fatal(err)
	}
	record := func(recordID, previous, fix string, a *board.ExecutionAuthorization) board.ReviewDisposition {
		return board.ReviewDisposition{RecordID: recordID, PreviousRecordID: previous, RunID: run.RunID, BatchID: run.BatchID, FindingID: h.evidence.FindingID, TaskID: h.task, Author: "claude", ReportHash: run.Hashes["report.md"], Original: "needs a fix", Status: "fixed", Basis: "fixture fix", FixCommit: fix, Verification: "fixture verification", Authorization: a}
	}
	submit := func(key string, d board.ReviewDisposition) error {
		card, err := h.holder.Show(t.Context(), h.task)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.holder.ReviewAction(t.Context(), key, "disposition", server.ReviewDispositionSubmission{Disposition: d, ExpectedRevision: card.Revision}, &h.proof)
		return err
	}
	before, after := strings.Repeat("c", 40), strings.Repeat("d", 40)
	if err := submit("fix", record("fix", "", before, nil)); err != nil {
		t.Fatalf("the first record: %v", err)
	}

	card, err := h.holder.Show(t.Context(), h.task)
	if err != nil {
		t.Fatal(err)
	}
	h.card = card
	input := h.input("correction-round")
	if err := h.prepare("prepare-correction", input); err != nil {
		t.Fatal(err)
	}
	prepared, err := h.holder.Dispatch(t.Context(), h.task, input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.holder.Move(t.Context(), "accept-correction", h.task, api.Move{State: "working", Authorization: prepared.Authorization}, &h.proof); err != nil {
		t.Fatal(err)
	}
	if err := submit("refused", record("fix-rebased", "fix", after, nil)); err == nil {
		t.Fatal("the service took a correction record without the round authorization")
	}
	if err := submit("correction", record("fix-rebased", "fix", after, &prepared.Authorization)); err != nil {
		t.Fatalf("the accepted sync round refused its correction record: %v", err)
	}
	v, err := board.ReadReviewBatchView(root, run.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	var fixCommit string
	for _, d := range v.Runs[0].Records {
		if d.RecordID == "fix-rebased" {
			fixCommit = d.FixCommit
		}
	}
	if len(v.Runs[0].Records) != 2 || fixCommit != after {
		t.Fatalf("the service did not keep both records: %+v", v.Runs[0].Records)
	}
}
