package liveness

import (
	"context"
	"errors"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// A missing terminal program is named by the backend that owns the WINDOW,
// not by the herdr or tmux wording.
func TestClassifyNamesMissingTerminalProgram(t *testing.T) {
	resetLang(t)
	original := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = original }()
	for window, program := range map[string]string{"luvus:7:7": "luvus", "herdr:w1:t1:w1:p1": "herdr", "tmux:$1:@1:%1": "tmux"} {
		rep := ClassifyTaskContext(context.Background(), board.Entry{TaskID: "missing"}, "- SESSION: codex wanted\n- WINDOW: "+window+"\n")
		if want := config.Text("terminal.executable_not_in_path", program); rep.Status != Unknown || rep.Detail != want {
			t.Fatalf("%s: report=%+v; want %q", window, rep, want)
		}
	}
}
