package launch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestStartCollectsJournalWarningsThroughSuccessAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launcher fixtures use POSIX executables")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			root, _, _ := setupBoard(t)
			id, _ := makeTodo(t, root, "journal-warnings")
			legacy := filepath.Join(root, ".kander", "operations", "legacy.json")
			if err := os.WriteFile(legacy, []byte(`{"schema":1,"operation_id":"legacy","phase":"committed"}`), 0600); err != nil {
				t.Fatal(err)
			}
			// A malformed staging entry makes best-effort cleanup warn after each
			// committed write without failing the card mutation or launcher.
			if err := os.WriteFile(filepath.Join(root, ".kander", "migrations", "cleanup-blocked"), []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			var preview StartPreview
			out, stderr, err := capture(t, func() error { var e error; preview, e = PreviewStart(root, id); return e })
			if err != nil || out != "" || stderr != "" || len(preview.Warnings) != 1 {
				t.Fatalf("preview: %+v %q %q %v", preview, out, stderr, err)
			}
			if fail {
				oldWrite := writeDocumentFn
				t.Cleanup(func() { writeDocumentFn = oldWrite })
				writeDocumentFn = func(string, board.Entry, string) error { return errors.New("injected metadata failure") }
			}
			var result StartResult
			out, stderr, err = capture(t, func() error { var e error; result, e = Start(root, "claude", "tmux", id); return e })
			if (err != nil) != fail || out != "" || stderr != "" {
				t.Fatalf("start wrote terminal output or changed outcome: %q %q %v", out, stderr, err)
			}
			warnings := strings.Join(result.Warnings, "\n")
			if len(result.Warnings) != 2 || !strings.Contains(warnings, "kander init") || !strings.Contains(warnings, "cleanup-blocked") {
				t.Fatalf("missing or repeated journal warnings: %v", result.Warnings)
			}
			var log board.WarningLog
			snapshot, err := board.ReadSnapshotWithWarnings(root, id, &log)
			want := "working"
			if fail {
				want = "todo"
			}
			if err != nil || snapshot.Entry.State != want {
				t.Fatalf("lost committed/rolled-back result: %+v %v", snapshot, err)
			}
		})
	}
}
