package liveness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestClassifyTaskSharesForwardAndReverseDeadline(t *testing.T) {
	for _, channel := range []string{"herdr", "tmux"} {
		t.Run(channel, func(t *testing.T) {
			resetLang(t)
			installPOSIXFakes(t, true)
			script := "#!/bin/sh\n"
			window := "tmux:$1:@1:%1"
			if channel == "herdr" {
				window = "herdr:w1:t1:w1:p1"
				script += `if [ "$2" = get ]; then
 /bin/sleep 0.3
 echo '{"error":{"code":"pane_not_found"}}' >&2
 exit 1
fi
`
			} else {
				script += `if [ "$1" = display-message ]; then
 /bin/sleep 0.3
 echo "can't find pane" >&2
 exit 1
fi
`
			}
			marker := filepath.Join(t.TempDir(), "reverse-started")
			t.Setenv("PROBE_REVERSE_STARTED", marker)
			script += "echo started > \"$PROBE_REVERSE_STARTED\"\nexec /bin/sleep 3\n"
			if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), channel), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			text := "- SESSION: codex wanted\n- WINDOW: " + window + "\n"
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			started := time.Now()
			rep := ClassifyTaskContext(ctx, board.Entry{TaskID: "budget"}, text)
			elapsed := time.Since(started)
			if rep.Status != Unknown || rep.NewWindow != "" || !strings.Contains(rep.Detail, "反查:") || !strings.Contains(rep.Detail, "探测期限已耗尽") {
				t.Fatalf("report=%+v", rep)
			}
			if elapsed < 400*time.Millisecond || elapsed > 650*time.Millisecond {
				t.Fatalf("elapsed=%s; shared budget=400ms", elapsed)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("reverse stage was not reached: %v", err)
			}
		})
	}
}

func TestHerdrRevalidationUsesRemainingBudget(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	script := `#!/bin/sh
if [ "$2" = list ]; then
 /bin/sleep 0.15
 echo '{"result":{"panes":[{"tab_id":"w1:t2","pane_id":"w1:p2","agent":"codex","agent_session":{"value":"wanted"}}]}}'
elif [ "$3" = w1:p1 ]; then
 /bin/sleep 0.15
 echo '{"error":{"code":"pane_not_found"}}' >&2
 exit 1
else
 echo started > "$PROBE_RECHECK_STARTED"
 exec /bin/sleep 3
fi
`
	marker := filepath.Join(t.TempDir(), "recheck")
	t.Setenv("PROBE_RECHECK_STARTED", marker)
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	rep := ClassifyTaskContext(ctx, board.Entry{TaskID: "recheck"}, "- SESSION: codex wanted\n- WINDOW: herdr:w1:t1:w1:p1\n")
	if elapsed := time.Since(started); elapsed > 650*time.Millisecond {
		t.Fatalf("elapsed=%s", elapsed)
	}
	if rep.Status != Unknown || rep.NewWindow != "" || !strings.Contains(rep.Detail, "探测期限已耗尽") {
		t.Fatalf("report=%+v", rep)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("recheck not reached: %v", err)
	}
}

func TestClassifyCanceledBeforeLookupDoesNotLaunch(t *testing.T) {
	resetLang(t)
	original := lookPath
	lookPath = func(string) (string, error) { t.Fatal("looked up an executable with no budget"); return "", nil }
	defer func() { lookPath = original }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep := ClassifyTaskContext(ctx, board.Entry{TaskID: "canceled"}, "- SESSION: codex wanted\n- WINDOW: tmux:$1:@1:%1\n")
	if rep.Status != Unknown || !strings.Contains(rep.Detail, "探测已取消") {
		t.Fatalf("report=%+v", rep)
	}
}

func TestClassifyCancellationDuringReverseLookup(t *testing.T) {
	for _, channel := range []string{"herdr", "tmux"} {
		t.Run(channel, func(t *testing.T) {
			resetLang(t)
			installPOSIXFakes(t, true)
			marker := filepath.Join(t.TempDir(), "started")
			t.Setenv("PROBE_REVERSE_STARTED", marker)
			script := "#!/bin/sh\n"
			window := "tmux:$1:@1:%1"
			if channel == "herdr" {
				window = "herdr:w1:t1:w1:p1"
				script += `if [ "$2" = get ]; then
 echo '{"error":{"code":"pane_not_found"}}' >&2
 exit 1
fi
`
			} else {
				script += `if [ "$1" = display-message ]; then
 echo "can't find pane" >&2
 exit 1
fi
`
			}
			script += "echo ready > \"$PROBE_REVERSE_STARTED\"\nexec /bin/sleep 3\n"
			if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), channel), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan Report, 1)
			go func() {
				done <- ClassifyTaskContext(ctx, board.Entry{TaskID: "cancel-reverse"}, "- SESSION: codex wanted\n- WINDOW: "+window+"\n")
			}()
			ready := false
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
				if _, err := os.Stat(marker); err == nil {
					ready = true
					break
				}
				time.Sleep(time.Millisecond)
			}
			started := time.Now()
			cancel()
			rep := <-done
			if !ready {
				t.Fatal("reverse lookup did not start")
			}
			if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
				t.Fatalf("cancel took %s", elapsed)
			}
			if rep.Status != Unknown || rep.NewWindow != "" || !strings.Contains(rep.Detail, "探测已取消") || !strings.Contains(rep.Detail, "反查:") {
				t.Fatalf("report=%+v", rep)
			}
		})
	}
}
