package server

import (
	"encoding/json"
	"reflect"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"net/http"
	"testing"
)

func TestFinishPreservesTheResponseCommittedByTheDomainTransaction(t *testing.T) {
	p := &project{root: t.TempDir()}
	pending := receipt{Key: "operation", Operation: "producer", Digest: "request-digest", Status: receiptPending}
	committed := pending
	committed.Status, committed.HTTP, committed.Body = receiptDone, http.StatusOK, json.RawMessage(`{"revision":12,"observed_at":"committed-time"}`)
	path := receiptPath(pending.Key)
	if err := writeJSON(p.root, path, committed); err != nil {
		t.Fatal(err)
	}
	if _, err := readJSON(p.root, path, &committed); err != nil {
		t.Fatal(err)
	}
	// A post-commit read may observe a different clock or already renewed claim.
	out := p.finish(path, pending, outcome{status: http.StatusOK, body: map[string]any{"revision": 12, "observed_at": "later-time"}})
	if out.err != nil || out.status != http.StatusOK {
		t.Fatalf("finish: %+v", out)
	}
	raw, ok := out.body.(json.RawMessage)
	if !ok || string(raw) != string(committed.Body) {
		t.Fatalf("transaction result was replaced: %s", raw)
	}
	var stored receipt
	if _, err := readJSON(p.root, path, &stored); err != nil || string(stored.Body) != string(committed.Body) {
		t.Fatalf("committed response was rewritten: %+v %v", stored, err)
	}
}

func TestPendingBoardRecoveryDoesNotPoisonTheOperationReceipt(t *testing.T) {
	p := &project{root: t.TempDir()}
	original := receipt{Schema: receiptSchema, Key: "pending-operation", Operation: "new", Digest: "original-digest", Status: receiptPending}
	path := receiptPath(original.Key)
	if err := writeJSON(p.root, path, original); err != nil {
		t.Fatal(err)
	}
	out := p.finish(path, original, outcome{err: &board.Error{Code: "board.transaction_pending", Message: "recovery required"}})
	if out.err == nil || asAPIError(out.err).Code != api.CodeNotReady || asAPIError(out.err).Status != http.StatusServiceUnavailable {
		t.Fatalf("pending recovery was cached as a domain rejection: %+v", out)
	}
	var after receipt
	if _, err := readJSON(p.root, path, &after); err != nil || !reflect.DeepEqual(original, after) {
		t.Fatalf("pending receipt changed: %+v %v", after, err)
	}
}
