package worker

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

func TestRestartLoopClosesTheRequestFilesOfTheOldLoop(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("restart-files", true)
	r := h.request(c, "typed before the restart")
	h.workAfter(16 * time.Second)
	dir := h.loop.deliveries[keyOf(c)].files[r.ID].dir.Path
	old := h.loop
	h.restartLoop()
	if h.loop == old || len(old.deliveries) != 0 {
		t.Fatal("the helper replaces the loop after closing the old one")
	}
	requireRequestDirRemoved(t, dir)
}

// Scenario R1: the worker starts again after the fallback committed and its
// audit failed.
func TestRestartReplaysTheCommittedFallback(t *testing.T) {
	rig, fallback := scenarioP(t, "restart-r1", gatewayAuditFails)
	rig.h.restartLoop()
	files := rig.h.countRequestFiles()
	results := len(rig.g.sendsOf(rig.r.ID, ackedNote))
	rig.h.workAfter(time.Second)
	sends := rig.g.sendsOf(rig.r.ID, ackedNote)
	if len(sends) != results+1 || sends[results].key != fallback.key || !bytes.Equal(sends[results].body, fallback.body) {
		t.Fatalf("the first round of the new loop sends the fallback once, as it was: %v", sends[results:])
	}
	if rig.g.audited(fallback.key) != 1 {
		t.Fatalf("the fallback key is audited once: %d", rig.g.audited(fallback.key))
	}
	rig.h.workAfter(answerBackoffMax)
	rig.h.workAfter(answerBackoffMax)
	if got := len(rig.g.sendsOf(rig.r.ID, ackedNote)); got != results+1 {
		t.Fatalf("later rounds send no result: %d sends, want %d", got, results+1)
	}
	if len(rig.h.lines) != 1 || files.Load() != 0 {
		t.Fatalf("the new loop delivered again: lines=%d request files=%d", len(rig.h.lines), files.Load())
	}
	rig.g.requireStableSends(t)
	rig.g.requireContentKeys(t, rig.c, rig.r.ID)
}

// Scenario R2: the typed note and the result committed; only the audit of the
// typed note failed when the worker starts again.
func TestRestartReplaysTheTypedNoteBeforeTheResult(t *testing.T) {
	rig := newReplayRig(t, "restart-r2", func(send gatewaySend, prior int) gatewayReply {
		if send.answer.Note == typedNote && prior == 0 {
			return gatewayReply{mode: gatewayAuditFails}
		}
		return gatewayReply{}
	})
	rig.typeAndAcknowledge(t)
	rig.h.workAfter(0)
	typed := rig.g.sendsOf(rig.r.ID, typedNote)
	current := rig.h.requests(rig.c)[0]
	if len(typed) != 1 || rig.g.audited(typed[0].key) != 0 || current.Note != ackedNote {
		t.Fatalf("the result committed while the typed audit is open: %v %+v", typed, current)
	}
	rig.h.restartLoop()
	files := rig.h.countRequestFiles()
	rig.h.workAfter(time.Second)
	typed = rig.g.sendsOf(rig.r.ID, typedNote)
	if len(typed) != 2 || typed[1].answer.ExpectedRevision != current.Revision-2 {
		t.Fatalf("the new loop rebuilds the typed note two revisions back: %v", typed)
	}
	requireOneAttempt(t, "the typed note", typed)
	if rig.g.audited(typed[0].key) != 1 {
		t.Fatalf("the typed key is audited once: %d", rig.g.audited(typed[0].key))
	}
	if got := rig.h.requests(rig.c)[0]; got.Status != current.Status || got.Note != current.Note || got.Revision != current.Revision {
		t.Fatalf("the rebuild changes nothing: %+v, want %+v", got, current)
	}
	if len(rig.h.lines) != 1 || files.Load() != 0 {
		t.Fatalf("the new loop delivered again: lines=%d request files=%d", len(rig.h.lines), files.Load())
	}
	rig.g.requireStableSends(t)
}

// Scenario R3: an older worker answered with random keys, so no content key
// was ever used and every rebuilt answer is refused.
func TestRestartAfterAnOlderWorkerChangesNothing(t *testing.T) {
	g := newGateway(nil)
	h := g.harness(t)
	p, c := h.claimed("restart-r3", true)
	r := h.request(c, "answered by an older worker")
	revision := r.Revision
	for _, note := range []string{reservationNote, typedNote, ackedNote} {
		answered, err := p.AnswerRequest(t.Context(), "", c.TaskID, r.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: revision, Note: note}, *c.Claim())
		if err != nil {
			t.Fatal(err)
		}
		revision = answered.Revision
	}
	before := h.requests(c)[0]
	written := len(g.ofRequest(r.ID))
	h.workAfter(time.Second)
	rebuilt := g.ofRequest(r.ID)[written:]
	if len(rebuilt) != 2 {
		t.Fatalf("the typed note and the result are rebuilt: %v", rebuilt)
	}
	for _, send := range rebuilt {
		if send.status != http.StatusConflict || send.code != api.CodeRevisionConflict {
			t.Fatalf("a rebuilt answer of an older worker is refused: %v", send)
		}
	}
	if got := h.requests(c)[0]; got.Status != before.Status || got.Note != before.Note || got.Revision != before.Revision {
		t.Fatalf("the rebuild changes nothing: %+v, want %+v", got, before)
	}
	if open := h.openAttempts(c); len(open) != 0 {
		t.Fatalf("no fallback follows a rebuilt result: %+v", open)
	}
	h.workAfter(answerBackoffMax)
	h.workAfter(answerBackoffMax)
	if got := len(g.ofRequest(r.ID)); got != written+2 {
		t.Fatalf("later rounds send nothing: %d sends, want %d", got, written+2)
	}
}
