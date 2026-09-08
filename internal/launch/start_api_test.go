package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func TestStartReturnsResultWithoutTerminalOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launcher fixtures use POSIX executables")
	}
	for _, launcher := range []string{"tmux", "tmux-session", "herdr"} {
		t.Run(launcher, func(t *testing.T) {
			root, _, bin := setupBoard(t)
			if launcher == "herdr" {
				installHerdr(t, root, bin)
				t.Setenv("HERDR_SOCKET_PATH", "")
			}
			id, _ := makeTodo(t, root, "start-result")
			var result StartResult
			out, stderr, err := capture(t, func() error { var e error; result, e = Start(root, "claude", launcher, id); return e })
			if err != nil {
				t.Fatal(err)
			}
			if out != "" || stderr != "" {
				t.Fatalf("unexpected output: %q %q", out, stderr)
			}
			if result.TaskID != id || result.Size != "small" || result.Agent != "claude" || result.Plan.Launcher != launcher || result.Outcome.Pane == "" {
				t.Fatalf("result=%+v", result)
			}
			if launcher == "herdr" && (result.Outcome.Tab == "" || len(result.Warnings) != 1) {
				t.Fatalf("missing address/warning: %+v", result)
			}
			if launcher != "herdr" && (result.Plan.Session == "" || result.Outcome.Window == "") {
				t.Fatalf("missing address: %+v", result)
			}
			snapshot, e := board.ReadSnapshot(root, id)
			if e != nil || snapshot.Entry.State != "working" {
				t.Fatalf("snapshot=%+v err=%v", snapshot, e)
			}
		})
	}
}

func TestPreviewStartResolvesSizeAndLauncherWithoutMutation(t *testing.T) {
	root, _, _ := setupBoard(t)
	for _, large := range []bool{false, true} {
		path, err := board.NewTask(root, "feature", "preview-"+map[bool]string{false: "small", true: "large"}[large], "Preview", "en", large)
		if err != nil {
			t.Fatal(err)
		}
		id := filepath.Base(path)
		before, _ := board.ReadSnapshot(root, id)
		for _, fallback := range []bool{false, true} {
			loadEffective = func() (*config.Config, error) {
				agents := map[string]string{"large": "grok", "small": "cursor"}
				if fallback {
					agents = nil
				}
				return envConfig("claude", "auto", agents), nil
			}
			t.Setenv("HERDR_ENV", "1")
			preview, err := PreviewStart(root, id)
			if err != nil {
				t.Fatal(err)
			}
			want := map[bool]string{false: "cursor", true: "grok"}[large]
			if fallback {
				want = "claude"
			}
			if preview.Agent != want || preview.Launcher != "herdr" || preview.Size != before.Entry.Kind || preview.State != "backlog" {
				t.Fatalf("preview=%+v", preview)
			}
		}
		after, _ := board.ReadSnapshot(root, id)
		if after.Text != before.Text || after.Revision != before.Revision || after.Entry.State != "backlog" {
			t.Fatal("preview changed card")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log")); !os.IsNotExist(err) {
		t.Fatalf("preview invoked tmux: %v", err)
	}
}

func TestStartRequiresExplicitTaskWithoutPrompt(t *testing.T) {
	root, _, _ := setupBoard(t)
	makeTodo(t, root, "no-prompt")
	out, stderr, err := capture(t, func() error { _, e := Start(root, "", "", ""); return e })
	if err == nil || out != "" || stderr != "" {
		t.Fatalf("out=%q stderr=%q err=%v", out, stderr, err)
	}
}
