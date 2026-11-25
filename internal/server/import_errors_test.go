package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// blockedSource returns a v1 board whose backlog card fails the import check,
// and the ID of that card.
func blockedSource(t *testing.T) (string, string) {
	t.Helper()
	source := v1Board(t)
	ids, _ := filepath.Glob(filepath.Join(source, "backlog", "*"))
	id := filepath.Base(ids[0])
	editSpec(t, source, "backlog", id, badPrerequisites)
	return source, id
}

// wantRejection checks that err is the coded API error of a refused import
// and that its message names the blocking card.
func wantRejection(t *testing.T, err error, id string) {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeBoardRejected || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a blocked import must be a board rejection: %v", err)
	}
	if !strings.Contains(apiErr.Message, "staged board fails check; nothing was published") || !strings.Contains(apiErr.Message, id) || !strings.Contains(apiErr.Message, importCheckHint) {
		t.Fatalf("the rejection must name the blocking card and the next step: %q", apiErr.Message)
	}
}

func TestImportRejectsABlockedBoardWithItsReason(t *testing.T) {
	source, id := blockedSource(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	_, err := s.Import("demo", "", source)
	wantRejection(t, err, id)
}

func TestImportRouteStreamsTheReasonOfABlockedBoard(t *testing.T) {
	source, id := blockedSource(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	status, _, lines, raw := streamImport(t, h.URL, s.adminToken, fmt.Sprintf(`{"project":"demo","from":%q}`, source))
	last := lines[len(lines)-1]
	if status != http.StatusOK || last.Error == nil || last.Status != http.StatusUnprocessableEntity || last.Error.Code != api.CodeBoardRejected || !strings.Contains(last.Error.Message, id) {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	c, _ := client.New(h.URL, "host-b")
	_, err := c.ImportWithProgress(t.Context(), s.adminToken, api.ImportRequest{Project: "demo", From: source}, func(api.ImportProgress) {})
	wantRejection(t, err, id)
}

func TestImportRouteAnswersTheReasonOfABlockedBoardInOneJSON(t *testing.T) {
	source, id := blockedSource(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	c, _ := client.New(h.URL, "host-b")
	_, err := c.Import(t.Context(), s.adminToken, api.ImportRequest{Project: "demo", From: source})
	wantRejection(t, err, id)
}

func TestImportRefusesASourceInsideTheDataDirectoryAsInvalid(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	_, err := s.Import("demo", "", filepath.Join(s.data, "kanban"))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInvalidRequest || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("an overlapping source must be an invalid request: %v", err)
	}
}
