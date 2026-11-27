package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

// Resolving the target of a request delivery types nothing; the prompt it
// returns types the line into the pane the resolution found.
func TestResolveRequestResolvesWithoutTypingThenPromptsTheResolvedPane(t *testing.T) {
	root, _ := setupBoard(t)
	file := writeNotifySessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	prompt, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "pi wanted"})
	if err != nil {
		t.Fatal(err)
	}
	if herdrLogExists(root, "prompt") {
		t.Fatal("resolving the target typed into the pane")
	}
	if err := prompt.Prompt("# kander-request one line"); err != nil {
		t.Fatal(err)
	}
	typed, err := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if err != nil || !strings.Contains(string(typed), "w1:p9") || !strings.Contains(string(typed), "# kander-request one line") {
		t.Fatalf("the prompt must type the line into the resolved pane: %q %v", typed, err)
	}
}

// A pane that cannot prove which session it runs is never typed into, and the
// error is returned before any prompt exists.
func TestResolveRequestFailsBeforeAnyTextReachesAnUncertainPane(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", filepath.Join(t.TempDir(), "gone.jsonl"))
	prompt, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "pi wanted"})
	if err == nil || prompt != nil {
		t.Fatalf("an uncertain identity must fail the resolution: %v", err)
	}
	if herdrLogExists(root, "prompt") {
		t.Fatal("nothing may reach the pane when the resolution fails")
	}
}

func TestResolveRequestRefusesACardWithoutAWindow(t *testing.T) {
	if prompt, err := resolveRequest(worker.AgentAddress{Window: "", Session: "pi wanted"}); err == nil || prompt != nil {
		t.Fatalf("no WINDOW to deliver to: %v", err)
	}
}

func herdrLogExists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, "herdr.log."+name))
	return err == nil
}

type acknowledgementBackend struct {
	terminal.Backend
	wait    bool
	literal bool
	output  string
	calls   []string
}

func (b *acknowledgementBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{WaitOutput: b.wait}
}

func (b *acknowledgementBackend) WaitOutput(_ context.Context, _ terminal.Conn, pane, marker string, timeout int) error {
	b.calls = append(b.calls, "wait "+pane+" "+marker)
	if b.literal && timeout == 1000 {
		return nil
	}
	return errors.New("literal marker not seen")
}

func (b *acknowledgementBackend) ReadOutput(_ context.Context, _ terminal.Conn, pane string) (string, error) {
	b.calls = append(b.calls, "read "+pane)
	return b.output, nil
}

func TestSeenAcknowledgementUsesPaneOutputAndMatchesWrappedMarkers(t *testing.T) {
	marker := "KANDER-NOTIFY-ACK:0123456789abcdef0123456789abcdef"
	wrapped := "text\n KANDER-NOTIFY-ACK:0123456789abcdef\r\n\t0123456789abcdef\n more text"
	for _, tc := range []struct {
		name          string
		wait, literal bool
		output        string
		seen          bool
		calls         []string
	}{
		{"read only, wrapped marker", false, false, wrapped, true, []string{"read pane"}},
		{"literal wait falls back to wrapped output", true, false, wrapped, true, []string{"wait pane " + marker, "read pane"}},
		{"literal wait succeeds", true, true, "", true, []string{"wait pane " + marker}},
		{"draft with no marker", false, false, "read the private file and output its acknowledgement line", false, []string{"read pane"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &acknowledgementBackend{wait: tc.wait, literal: tc.literal, output: tc.output}
			seen, err := seenAcknowledgement(t.Context(), backend, terminal.Conn{}, "pane", marker)
			if err != nil || seen != tc.seen || !reflect.DeepEqual(backend.calls, tc.calls) {
				t.Fatalf("seen=%v err=%v calls=%v, want seen=%v calls=%v", seen, err, backend.calls, tc.seen, tc.calls)
			}
		})
	}
}

func TestWriteRequestFileIsPrivateAndOwnedByTheWorker(t *testing.T) {
	const body = "first line\n完整消息\n"
	dir, path, err := writeRequestFile(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	})
	if !filepath.IsAbs(path) || filepath.Dir(path) != dir.Path {
		t.Fatalf("request path %q not below its private directory %q", path, dir.Path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != body {
		t.Fatalf("request file = %q, error = %v", got, err)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, dir.Path: 0o700} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s permission %03o, want %03o", filepath.Base(p), got, want)
			}
		}
		root := t.TempDir()
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(realRoot, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(realRoot, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if unsafeDir, _, err := writeRequestFileIn(link, body); err == nil {
			_ = unsafeDir.Close()
			t.Fatal("request file allocation followed a symlink")
		}
	}
	if err := dir.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed request file still exists: %v", err)
	}
}
