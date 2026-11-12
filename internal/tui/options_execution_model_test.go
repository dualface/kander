package tui

import (
	"github.com/dualface/kander/internal/config"
	"testing"
)

func TestExecutionModelInputSavesWhenAgentsMatch(t *testing.T) {
	// This scope-save assertion must not inherit the developer project overlay.
	t.Chdir(t.TempDir())
	_, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionExecution))
	large := panel.session.Config.KanbanAgents["large"]
	small := panel.session.Config.KanbanAgents["small"]
	if large == "" || large != small {
		t.Fatalf("need the same agent for large and small, got large=%q small=%q", large, small)
	}
	if len(panel.bind.modelFields) < 2 {
		t.Fatal("need multiple model fields so later appends can grow the binding slice")
	}
	before := panel.session.Config.Models.Kanban[large]["large_model"]
	drivePanel(panel, keyMsg("down"))
	if panel.form.GetFocusedField() != panel.bind.formFields[1] {
		t.Fatal("down should focus the large-task model input")
	}
	typeRune(panel, 'X')
	want := before + "X"
	assertTypedIntoView(t, panel, want)
	if got := panel.session.Config.Models.Kanban[large]["large_model"]; got != want {
		t.Fatalf("session large_model=%q want %q", got, want)
	}
	if !panel.dirty {
		t.Fatal("typing into the model input must mark unsaved changes")
	}
	drivePanel(panel, keyMsg("enter"))
	loaded, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Models.Kanban[large]["large_model"]; got != want {
		t.Fatalf("disk large_model=%q want %q", got, want)
	}
}
