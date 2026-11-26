package builtin_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	_ "github.com/dualface/kander/v2/internal/notify"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
	"github.com/dualface/kander/v2/internal/worker"
)

func TestTmuxWorkerSubmitsRealCodexRequests(t *testing.T) {
	conn, backend, pane, tmux := startRealCodexPane(t)
	// The production resolver runs tmux itself. Route its commands to the
	// test socket rather than to the user's tmux server.
	t.Setenv("TMUX", strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{socket_path},#{pid},0")))
	t.Setenv("TMUX_PANE", pane)
	ref := "request-test-" + randomHex(t, 4)
	tmux("set-option", "-p", "-t", pane, "@kander_session", ref)
	window := "tmux:" + strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{session_id}:#{window_id}:#{pane_id}"))

	isWorking := func(text string) bool {
		return strings.Contains(text, "esc to interrupt") || strings.Contains(text, "Working (")
	}
	t.Run("idle with 12 KiB of Chinese", func(t *testing.T) {
		deliverRealCodexRequest(t, window, ref, strings.Repeat("中", 4096), worker.Resolve)
	})
	t.Run("busy with another turn", func(t *testing.T) {
		if _, err := exec.LookPath("ps"); err != nil {
			t.Skipf("ps is needed to verify that Codex is still executing: %v", err)
		}
		waitPane(t, conn, backend, pane, 30*time.Second, func(text string) bool {
			return !isWorking(text) && strings.Contains(text, "? for shortcuts")
		})
		if err := backend.DeliverText(t.Context(), conn, pane, "Run sleep 25 in the shell now to simulate ongoing work. Then reply with BUSY-TURN-ENDED."); err != nil {
			t.Fatal(err)
		}
		pid := strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{pane_pid}"))
		runningCtx, stop := context.WithTimeout(t.Context(), 30*time.Second)
		defer stop()
		poll := time.NewTicker(100 * time.Millisecond)
		defer poll.Stop()
		for !paneRunningSleep(runningCtx, pid) {
			select {
			case <-runningCtx.Done():
				t.Fatal("Codex did not start the shell sleep")
			case <-poll.C:
			}
		}
		resolveWhileBusy := func(window, session string) (*worker.Target, error) {
			target, err := worker.Resolve(window, session)
			if err != nil {
				return nil, err
			}
			prompt := target.Prompt
			target.Prompt = func(line string) error {
				if !paneRunningSleep(t.Context(), pid) {
					t.Error("Codex had already finished the ongoing turn before worker delivery")
				}
				return prompt(line)
			}
			return target, nil
		}
		deliverRealCodexRequest(t, window, ref, "This is the queued request from the test.", resolveWhileBusy)
	})
}

// The Codex footer can change between model and tool events. Check the actual
// shell command under the dedicated pane's process instead of relying on it.
func paneRunningSleep(ctx context.Context, panePID string) bool {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,args=").Output()
	if err != nil {
		return false
	}
	parents := map[string]string{}
	var sleeps []string
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		parents[f[0]] = f[1]
		if slices.Equal(f[2:], []string{"sleep", "25"}) {
			sleeps = append(sleeps, f[0])
		}
	}
	for _, pid := range sleeps {
		for pid != "" && pid != "0" {
			if pid == panePID {
				return true
			}
			pid = parents[pid]
		}
	}
	return false
}

func deliverRealCodexRequest(t *testing.T, window, ref, message string, resolve func(string, string) (*worker.Target, error)) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	httpServer := httptest.NewServer(svc.Handler())
	t.Cleanup(httpServer.Close)
	ctx := t.Context()
	holder, err := client.New(httpServer.URL, "host-holder")
	if err != nil {
		t.Fatal(err)
	}
	project, err := holder.Register(ctx, "codex-e2e", "")
	if err != nil {
		t.Fatal(err)
	}
	board := holder.Project(project.ID)
	task, err := board.NewTask(ctx, "", api.NewTask{Type: "bug", Slug: "request-ack", Title: "Test pane acknowledgement", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(task.Text, "<FILL_IN>", "Test criteria")
	body = strings.Replace(body, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := board.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: body, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := board.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	const execution = "codex-e2e"
	session := "codex " + ref
	claim, err := board.Claim(ctx, "", task.TaskID, api.ClaimRequest{
		ClientID: "host-holder", Owner: "codex", ExecutionID: execution,
		Start: &api.StartInput{Window: window, Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := remote.WriteCredential(home, remote.Credential{
		ServerURL: httpServer.URL, ClientID: "host-holder", ProjectID: project.ID, TaskID: task.TaskID, ExecutionID: execution,
		Generation: claim.Claim.Generation, Token: claim.Token, TTL: claim.TTL, RenewInterval: claim.RenewInterval,
		Window: window, Session: session,
	}); err != nil {
		t.Fatal(err)
	}
	sender, err := client.New(httpServer.URL, "host-sender")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Project(project.ID).CreateRequest(ctx, "", task.TaskID, api.NewCollabRequest{
		Kind: api.RequestNote, TargetGeneration: claim.Claim.Generation,
		Message: "This is only a test. After printing the acknowledgement, do not run any Kander commands.\n\n" + message,
	}); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- worker.Serve(workerCtx, worker.Options{
			Home: home, Tick: 100 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(t.Output(), nil)), Resolve: resolve,
			Observe: func(context.Context, remote.Credential) api.AgentObservation {
				return api.AgentObservation{Status: "unknown"}
			},
		})
	}()
	t.Cleanup(func() {
		cancelWorker()
		if err := <-workerDone; err != nil {
			t.Error(err)
		}
	})

	// The worker enforces the 120-second window from typing. Allow time
	// for the worker to start, resolve the target and write the receipt.
	readCtx, stop := context.WithTimeout(ctx, 135*time.Second)
	defer stop()
	var last string
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		requests, err := board.Requests(readCtx, task.TaskID)
		if err == nil && len(requests) == 1 {
			last = requests[0].Note
			if last == "submitted: the agent acknowledged the request file" {
				return
			}
			if strings.Contains(last, "not acknowledged within 120s") {
				t.Fatalf("real Codex did not acknowledge the request: %s", last)
			}
		}
		select {
		case <-readCtx.Done():
			t.Fatalf("real Codex request did not reach acknowledged: note=%q error=%v", last, err)
		case <-ticker.C:
		}
	}
}
