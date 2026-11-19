package remote_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/remote"
)

// legacyServerTransport answers the creation of a note or wrap-up request the
// way a server that predates both kinds does: with its one combined message.
type legacyServerTransport struct {
	next   http.RoundTripper
	legacy *atomic.Bool
	sent   *atomic.Int32
}

func (l legacyServerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if l.legacy.Load() && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/requests") {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var input api.NewCollabRequest
		if json.Unmarshal(body, &input) == nil && (input.Kind == api.RequestNote || input.Kind == api.RequestWrapUp) {
			l.sent.Add(1)
			refusal, _ := json.Marshal(api.Error{Code: api.CodeInvalidRequest, Message: "request needs kind, client and a message of at most 64 KiB"})
			header := http.Header{"Content-Type": {"application/json"}, api.HeaderProtocol: {strconv.Itoa(api.ProtocolVersion)}}
			return &http.Response{StatusCode: http.StatusBadRequest, Header: header, Body: io.NopCloser(bytes.NewReader(refusal)), Request: r}, nil
		}
	}
	return l.next.RoundTrip(r)
}

func useLegacyServer(t *testing.T) (legacy *atomic.Bool, sent *atomic.Int32) {
	t.Helper()
	legacy, sent = &atomic.Bool{}, &atomic.Int32{}
	legacy.Store(true)
	saved := http.DefaultTransport
	http.DefaultTransport = legacyServerTransport{next: saved, legacy: legacy, sent: sent}
	t.Cleanup(func() { http.DefaultTransport = saved })
	return legacy, sent
}

// A server that predates the note kind refuses it; the client names the kind
// and asks for the upgrade, and the same message goes through once the server
// is upgraded.
func TestOldServerRefusesNoteAndWrapUpWithAnUpgradeMessage(t *testing.T) {
	f := newRequestReceiptFixture(t, false)
	legacy, sent := useLegacyServer(t)
	task := f.grant.Task.TaskID
	for _, kind := range []string{api.RequestNote, api.RequestWrapUp} {
		input := api.NewCollabRequest{Kind: kind, Message: "Hello " + kind, TargetGeneration: 1}
		_, err := f.sender.SendRequest(t.Context(), "key-"+kind, task, input, "logical-"+kind)
		if !api.IsCode(err, api.CodeInvalidRequest) || !strings.Contains(err.Error(), config.Text("remote.request_kind_server_upgrade", kind)) {
			t.Fatalf("%s on the old server: %v", kind, err)
		}
	}
	if sent.Load() != 2 {
		t.Fatalf("the old server saw %d creations", sent.Load())
	}

	legacy.Store(false)
	// The failed attempt left no pending receipt, so the same command runs again.
	input := api.NewCollabRequest{Kind: api.RequestNote, Message: "Hello note", TargetGeneration: 1}
	got, err := f.sender.SendRequest(t.Context(), "retry-note", task, input, "logical-note")
	if err != nil || got.Kind != api.RequestNote || got.Status != api.RequestOpen {
		t.Fatalf("note after the upgrade: %+v %v", got, err)
	}
	requests, err := f.holder.Requests(t.Context(), task)
	if err != nil || len(requests) != 1 || requests[0].Kind != api.RequestNote {
		t.Fatalf("requests after the upgrade: %+v %v", requests, err)
	}
}

// Only the legacy combined message of the two new kinds is turned into the
// upgrade message; every other failure stays as it is.
func TestRequestCreateErrorOnlyRewritesTheLegacyRejection(t *testing.T) {
	legacy := &api.Error{Code: api.CodeInvalidRequest, Message: "request needs kind, client and a message of at most 64 KiB", Status: 400}
	for _, kind := range []string{api.RequestNote, api.RequestWrapUp} {
		got := remote.RequestCreateError(kind, legacy)
		if !api.IsCode(got, api.CodeInvalidRequest) || got == error(legacy) || !strings.Contains(got.Error(), kind) {
			t.Errorf("%s: legacy rejection became %v", kind, got)
		}
	}
	if got := remote.RequestCreateError(api.RequestSync, legacy); got != error(legacy) {
		t.Errorf("sync: %v", got)
	}
	other := &api.Error{Code: api.CodeInvalidRequest, Message: "a note request carries no dispatch"}
	if got := remote.RequestCreateError(api.RequestNote, other); got != error(other) {
		t.Errorf("new server refusal was rewritten: %v", got)
	}
	if got := remote.RequestCreateError(api.RequestNote, nil); got != nil {
		t.Errorf("nil became %v", got)
	}
	for _, lang := range []string{"en", "ja", "cn"} {
		for _, id := range []string{"remote.request_kind_server_upgrade", "remote.request_message_empty", "remote.request_message_too_long"} {
			text := i18n.Text(lang, id, "note")
			if text == "" || text == id || !i18n.Has(id) {
				t.Errorf("%s: no text for %s", lang, id)
			}
		}
		if text := i18n.Text(lang, "remote.request_kind_server_upgrade", "note"); !strings.Contains(text, "note") {
			t.Errorf("%s: the upgrade text does not name the kind: %q", lang, text)
		}
	}
}

func TestCheckRequestMessage(t *testing.T) {
	for _, message := range []string{"", " ", "\t\n  "} {
		if err := remote.CheckRequestMessage(message); err == nil {
			t.Errorf("blank message %q accepted", message)
		}
	}
	if err := remote.CheckRequestMessage(strings.Repeat("a", api.MaxRequestMessage)); err != nil {
		t.Errorf("a message at the bound: %v", err)
	}
	if err := remote.CheckRequestMessage(strings.Repeat("a", api.MaxRequestMessage+1)); err == nil {
		t.Error("a message over the bound was accepted")
	}
	// The bound counts bytes, not characters.
	if err := remote.CheckRequestMessage(strings.Repeat("é", api.MaxRequestMessage/2+1)); err == nil {
		t.Error("a multi-byte message over the bound was accepted")
	}
}

// An unresolved receipt that an older client saved for a plain notification
// has kind sync; the new client reconciles it against the stored request and
// never recomputes the kind.
func TestPendingReceiptOfAnOlderClientStillReconciles(t *testing.T) {
	f := newRequestReceiptFixture(t, true)
	input := api.NewCollabRequest{Kind: api.RequestSync, Message: "Plain notification from an older client", TargetGeneration: 1}
	if _, err := f.sender.SendRequest(t.Context(), "older-operation", f.grant.Task.TaskID, input, "older-logical"); err == nil {
		t.Fatal("outage was not surfaced")
	}
	f.blockFacts.Store(false)
	result, found, err := f.sender.RecoverRequest(t.Context(), "older-logical")
	if err != nil || !found || result.Kind != api.RequestSync || result.ID != api.CollaborationRequestID("older-operation") {
		t.Fatalf("older receipt: %+v %v %v", result, found, err)
	}
}
