package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

func withoutJournalOutput(t *testing.T, run func()) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	out, stderr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = out, stderr; _ = file.Close() }()
	os.Stdout, os.Stderr = file, file
	run()
	info, err := file.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatalf("journal bypassed TUI output channel: %v %v", info, err)
	}
}

func TestJournalWarningsReachRefreshDetailAndBacklogStart(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path, err := board.NewTask(root, "feature", "journal-notice", "Journal notice", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(path)
	before, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(before.Text, "<FILL_IN>", "Ready")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: Ready\n", 1)
	if err := os.WriteFile(filepath.Join(path, "spec.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".kander", "operations", "legacy.json"), []byte(`{"schema":1,"operation_id":"legacy","phase":"committed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".kander", "migrations", "cleanup-blocked"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("HERDR_ENV", "")
	t.Setenv("HERDR_WORKSPACE_ID", "")
	app := startTestApp("backlog")
	app.GetBoard = func() (BoardPayload, error) { return loadBoardPayload(root) }
	app.GetTask = func(id string) (Task, error) { return loadTaskPayload(root, id) }
	withoutJournalOutput(t, func() {
		app.refreshBoard()
		if !strings.Contains(app.CopyNotice, "kander init") {
			t.Fatal("refresh warning not visible")
		}
		app.openDetail()
		if app.Detail == nil || len(app.Detail.Warnings) != 1 {
			t.Fatal("detail warning not retained")
		}
		result, err := runTaskStart(startRequest{root: root, StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Agent: "claude", Launcher: "herdr"}})
		if err == nil {
			t.Fatal("missing expected launcher preflight error")
		}
		app.applyStartResult(startResult{result: result, err: err})
		if !strings.Contains(app.CopyNotice, "cleanup-blocked") || !strings.Contains(app.CopyNotice, "kander init") {
			t.Fatalf("start warnings not visible: %s", app.CopyNotice)
		}
		var warnings board.WarningLog
		after, e := board.ReadSnapshotWithWarnings(root, id, &warnings)
		if e != nil || after.Entry.State != "todo" || after.Text != text {
			t.Fatalf("cleanup warning changed committed backlog move: %+v %v", after, e)
		}
	})
}
