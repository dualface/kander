//go:build unix

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

func TestConfirmDoesNotReplayReceiptBeforeBoardCommit(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory write restriction cannot inject a rename failure as root")
	}
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, card := prepareRequestCard(t, h.URL)
	proj, err := s.project(p.ID())
	if err != nil {
		t.Fatal(err)
	}
	cursor := proj.events.cursor()
	const key = "interrupted-confirmation"
	post := func(s *Server) *httptest.ResponseRecorder {
		return postRecoveryConfirmation(t, s, p.ID(), key, card)
	}
	// Only the rename into todo needs write access to this directory. Validation,
	// preparation and the earlier service receipt write can all finish.
	todo := filepath.Join(proj.board, "todo")
	if err = os.Chmod(todo, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(todo, 0o700) })
	first := post(s)
	var saved receipt
	found, err := readJSON(proj.root, receiptPath(key), &saved)
	if err != nil || !found || saved.Status != receiptDone || saved.HTTP != http.StatusOK {
		t.Fatalf("failure did not reach the staged-success boundary: status=%q, HTTP=%d, found=%t, err=%v", saved.Status, saved.HTTP, found, err)
	}
	if _, err = os.Stat(filepath.Join(proj.board, "backlog", card.TaskID)); err != nil {
		t.Fatalf("card already left backlog: %v", err)
	}
	retry := post(s)
	for label, response := range map[string]*httptest.ResponseRecorder{"initial": first, "retry": retry} {
		var failure api.Error
		if response.Code != http.StatusServiceUnavailable || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != api.CodeNotReady {
			t.Errorf("%s returned HTTP %d: %.200s, want 503 not_ready", label, response.Code, response.Body.String())
		}
	}
	if got := proj.events.read(t.Context(), cursor, 0); got.Reset || len(got.Events) != 0 {
		t.Fatalf("uncommitted confirmation emitted an event: %+v", got)
	}
	if err = os.Chmod(todo, 0o700); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, h = openTest(t, dir, nil)
	replay := post(s)
	if replay.Code != http.StatusOK || replay.Body.String() != string(saved.Body)+"\n" {
		t.Fatalf("recovered reply changed: HTTP %d, %.200s", replay.Code, replay.Body.String())
	}
	c, err := client.New(h.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	p = c.Project(p.ID())
	page, err := p.Events(t.Context(), cursor, 0)
	if err != nil || !page.Reset {
		t.Fatalf("restart did not reset the old event cursor: %+v, %v", page, err)
	}
	list, err := p.List(t.Context(), "todo")
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].TaskID != card.TaskID || list.Tasks[0].Revision != card.Revision+1 {
		t.Fatalf("recovered list: %+v, %v", list, err)
	}
	shown, err := p.Show(t.Context(), card.TaskID)
	body, _ := board.SectionBody(shown.Text, board.SectionConfirmations)
	if actor, ok := board.LastConfirmer(shown.Text); err != nil || !ok || actor != "Alice" || strings.Contains(body, "\n") {
		t.Fatalf("recovered card confirmation: %q, %v", body, err)
	}
}
