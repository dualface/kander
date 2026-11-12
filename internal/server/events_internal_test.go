package server

import (
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/api"
)

func TestEventAppendFailureInvalidatesTheObservableCursor(t *testing.T) {
	root := t.TempDir()
	log, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.append(api.Event{Kind: api.EventTaskChanged, TaskID: "20261112-event-task"}); err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	log.rel = filepath.Join("missing-directory", "events.jsonl")
	if err := log.append(api.Event{Kind: api.EventTaskChanged, TaskID: "20261112-event-task"}); err == nil {
		t.Fatal("injected persistence failure did not occur")
	}
	page := log.read(t.Context(), cursor, 0)
	if !page.Reset || page.Cursor == cursor {
		t.Fatalf("a missed commit was invisible: %+v", page)
	}
}
func TestEventStreamRestartResetsEvenWithoutAnAppendedEvent(t *testing.T) {
	root := t.TempDir()
	log, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	cursor := log.cursor()
	// A card transaction may commit here and the process may die before append.
	restarted, err := openEventLog(root)
	if err != nil {
		t.Fatal(err)
	}
	page := restarted.read(t.Context(), cursor, 0)
	if !page.Reset {
		t.Fatal("restart silently resumed across an unobservable commit")
	}
}
