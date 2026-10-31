package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

// A takeover no launcher ended stands in for any unfinished start attempt.
func TestNotifyRefusesAnUnfinishedStart(t *testing.T) {
	for _, status := range []string{board.ExecutionPending, board.ExecutionUnresolved} {
		for name, options := range map[string]launch.DispatchOptions{"message": {}, "dispatch": {ID: "live-guard", Kind: "sync", Base: strings.Repeat("a", 40)}} {
			t.Run(status+"/"+name, func(t *testing.T) {
				root, task := tmuxNotifyFixture(t, "notify-unfinished")
				s, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = board.BindExecutionCursor(root, s.Entry); err != nil {
					t.Fatal(err)
				}
				in := board.TakeoverInput{Owner: board.MetadataFrom(s.Text, board.FieldOwner), Session: board.MetadataFrom(s.Text, board.FieldSession), Window: board.MetadataFrom(s.Text, board.FieldWindow)}
				if _, err = board.BeginTakeover(root, s.Entry, in); err != nil {
					t.Fatal(err)
				}
				if status == board.ExecutionUnresolved {
					if _, err = board.UnresolveExecution(root, s.Entry, "", "late failure"); err != nil {
						t.Fatal(err)
					}
				}
				before, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}

				out, _, err := capture(t, func() error { return commandNotify(root, task, "x", "", "", true, 61, options) })

				if err == nil || !strings.Contains(err.Error(), "--adopt --decision") || !strings.Contains(err.Error(), "--agent <agent> --decision") {
					t.Fatalf("err=%v", err)
				}
				if strings.Contains(out, "live-guard") {
					t.Fatalf("out=%s", out)
				}
				if _, err = board.ReadDispatch(root, task, "live-guard"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("a dispatch bound: %v", err)
				}
				after, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				if after.Revision != before.Revision || after.Text != before.Text {
					t.Fatal("card changed")
				}
				if _, err = os.Stat(filepath.Join(root, "tmux.log.instruction")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("delivered: %v", err)
				}
			})
		}
	}
}
