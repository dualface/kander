package remote_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

type requestReceiptFixture struct {
	sender     *remote.Session
	holder     *client.Project
	grant      api.ClaimGrant
	blockFacts atomic.Bool
}

func newRequestReceiptFixture(t *testing.T, blockFacts bool) *requestReceiptFixture {
	t.Helper()
	f := &requestReceiptFixture{}
	f.blockFacts.Store(blockFacts)
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderClientID) == "sender" && strings.HasSuffix(r.URL.Path, "/requests") {
			if r.Method == http.MethodPost {
				// Commit the real domain operation, then lose its response.
				handler.ServeHTTP(httptest.NewRecorder(), r)
			} else if !f.blockFacts.Load() {
				handler.ServeHTTP(w, r)
				return
			}
			w.Header().Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeInternal, Message: "injected response/facts outage"})
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	holder, err := client.New(httpServer.URL, "holder")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := client.New(httpServer.URL, "sender")
	if err != nil {
		t.Fatal(err)
	}
	sender.Retries = 0
	info, err := holder.Register(t.Context(), "request-receipt", "")
	if err != nil {
		t.Fatal(err)
	}
	f.holder = holder.Project(info.ID)
	card, err := f.holder.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "request-receipt", Title: "Recover a request without repeating delivery", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	card, err = f.holder.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.holder.Move(t.Context(), "", card.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	f.grant, err = f.holder.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "holder", ExecutionID: "request-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "client")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	f.sender = &remote.Session{ServerURL: httpServer.URL, Client: sender, Project: sender.Project(info.ID), Info: info, Home: home}
	return f
}

func TestRequestReceiptInspectsFactsAfterResponseLoss(t *testing.T) {
	f := newRequestReceiptFixture(t, false)
	input := api.NewCollabRequest{Kind: api.RequestSync, Message: "Inspect the saved request", TargetGeneration: 1}
	result, err := f.sender.SendRequest(t.Context(), "saved-operation", f.grant.Task.TaskID, input, "one-logical-message")
	if err != nil || result.ID != api.CollaborationRequestID("saved-operation") || result.Status != api.RequestOpen {
		t.Fatalf("fact recovery: %+v %v", result, err)
	}
	requests, err := f.holder.Requests(t.Context(), f.grant.Task.TaskID)
	if err != nil || len(requests) != 1 {
		t.Fatalf("response loss duplicated request: %+v %v", requests, err)
	}
}

func TestRequestReceiptRestartNeverRebindsUncertaintyToSuccessor(t *testing.T) {
	f := newRequestReceiptFixture(t, true)
	logical := "one-logical-message"
	input := api.NewCollabRequest{Kind: api.RequestSync, Message: "Inspect the saved request", TargetGeneration: 1}
	if _, err := f.sender.SendRequest(t.Context(), "saved-operation", f.grant.Task.TaskID, input, logical); err == nil {
		t.Fatal("outage was not surfaced")
	}
	proof := client.Claim{Token: f.grant.Token, Generation: f.grant.Claim.Generation}
	if _, err := f.holder.ObserveAgent(t.Context(), "stopped-fixture", f.grant.Task.TaskID, api.AgentObservation{Status: "stopped"}, proof); err != nil {
		t.Fatal(err)
	}
	state, err := f.holder.ExecutionState(t.Context(), f.grant.Task.TaskID, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.holder.Execution(t.Context(), "release-for-successor", f.grant.Task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "holder releases stopped execution for successor", Presence: &api.AgentObservation{Status: "stopped"}}, proof); err != nil {
		t.Fatal(err)
	}
	successor, err := client.New(f.sender.ServerURL, "successor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := successor.Project(f.sender.Info.ID).Takeover(t.Context(), "successor", f.grant.Task.TaskID, api.ClaimRequest{ClientID: "successor", ExecutionID: "new-execution", Owner: "codex", Decision: "explicit successor fixture", ExpectedGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	f.blockFacts.Store(false)
	// A fresh process keeps the private operation receipt, not an in-memory key.
	connection, err := client.New(f.sender.ServerURL, "sender")
	if err != nil {
		t.Fatal(err)
	}
	connection.Retries = 0
	restarted := &remote.Session{ServerURL: f.sender.ServerURL, Client: connection, Project: connection.Project(f.sender.Info.ID), Info: f.sender.Info, Home: f.sender.Home}
	result, found, err := restarted.RecoverRequest(t.Context(), logical)
	if err != nil || !found || result.TargetGeneration != 1 || result.Status != api.RequestSuperseded {
		t.Fatalf("uncertainty was rebound: %+v %v %v", result, found, err)
	}
	requests, err := f.holder.Requests(t.Context(), f.grant.Task.TaskID)
	if err != nil || len(requests) != 1 {
		t.Fatalf("recovery submitted to successor: %+v %v", requests, err)
	}
}
