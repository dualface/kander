package server_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
)

// The single-task reads scan only the requested ID; a missing ID must answer
// 404 not_found and log nothing as an internal failure.
func TestMissingTaskReadsAreNotFound(t *testing.T) {
	f, logs := startLogged(t, &clock{})
	p := f.project("host-a")
	const missing = "20261128-missing-task"
	_, showErr := p.Show(t.Context(), missing)
	_, documentErr := p.Document(t.Context(), missing, "report.md")
	_, decisionsErr := p.Decisions(t.Context(), missing)
	for name, err := range map[string]error{"show": showErr, "document": documentErr, "decisions": decisionsErr} {
		var e *api.Error
		if !errors.As(err, &e) || e.Code != api.CodeNotFound || e.Status != http.StatusNotFound || e.BoardCode != "board.task_does_not_exist" {
			t.Fatalf("%s: %#v", name, err)
		}
	}
	if text := logs.String(); strings.Contains(text, "level=ERROR") || strings.Contains(text, "request failed") {
		t.Fatalf("missing task reads logged an internal failure:\n%s", text)
	}
}
