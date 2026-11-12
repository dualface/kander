package launch

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

func TestImplicitNativeResumeReopensFrozenHandoffAfterUnknownResponse(t *testing.T) {
	skipOnWindows(t)
	cfg, direct, task, _, _ := remoteLifecycleFixture(t, "implicit-native-handoff-recovery")
	origin := cfg.Server.URL
	var hide atomic.Bool
	var handoffs atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), r.Method, origin+r.URL.RequestURI(), r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		req.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer response.Body.Close()
		if strings.HasSuffix(r.URL.Path, "/takeover") {
			handoffs.Add(1)
			if hide.Load() {
				_, _ = io.Copy(io.Discard, response.Body)
				http.Error(w, "hidden committed handoff", 503)
				return
			}
		}
		for key, values := range response.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	cfg.Server.URL = proxy.URL
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	local, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	markRemoteExecutorStopped(t, local, task.TaskID)
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	hide.Store(true)
	args := []string{task.TaskID, "--message", "Continue once after a confirmed local stop"}
	if code := RunResume(args); code == 0 {
		t.Fatal("hidden response reported success")
	}
	current, err := direct.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.Claim.Generation != 2 || current.Claim.Status != api.ClaimActive {
		t.Fatalf("handoff=%+v %v", current, err)
	}
	frozen := current.Claim.ExecutionID
	hide.Store(false)
	// Reopening the command happens after the current generation has changed.
	// It must reconcile the old frozen handoff, not queue a new proposal.
	for range 2 {
		if code := RunResume(args); code == 0 {
			t.Fatal("unlaunched recovery reported a new start")
		}
	}
	current, err = direct.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.Claim.Generation != 2 || current.Claim.ExecutionID != frozen || current.Claim.Status != api.ClaimLost {
		t.Fatalf("recovered=%+v %v", current, err)
	}
	after, _ := os.ReadFile(log)
	if !bytes.Equal(before, after) {
		t.Fatal("unknown handoff retry launched a terminal")
	}
	requests, err := direct.Project.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 0 {
		t.Fatalf("retry retargeted a request: %+v %v", requests, err)
	}
	if handoffs.Load() < 2 {
		t.Fatal("did not replay the original service handoff")
	}
}

func TestCompletedRemoteLaunchReceiptNeverRestoresReleasedAuthority(t *testing.T) {
	skipOnWindows(t)
	_, session, task, _, _ := remoteLifecycleFixture(t, "completed-launch-receipt")
	first, err := StartRemote("claude", "tmux", task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	old, _, _ := session.Credential(task.TaskID)
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	// A terminal transition ends authority; the completed private receipt remains.
	if _, err := session.Project.Move(t.Context(), client.NewKey(), task.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "test"}, old.Claim()); err != nil {
		t.Fatal(err)
	}
	if err := remote.RemoveCredential(session.Home, old); err != nil {
		t.Fatal(err)
	}
	second, err := StartRemote("claude", "tmux", task.TaskID)
	if err != nil || !second.Outcome.Replayed || second.executionID != old.ExecutionID || second.TaskID != first.TaskID {
		t.Fatalf("receipt=%+v %v", second, err)
	}
	if _, found, err := session.Credential(task.TaskID); err != nil || found {
		t.Fatalf("restored bearer: %v %v", found, err)
	}
	after, _ := os.ReadFile(log)
	if !bytes.Equal(before, after) {
		t.Fatal("completed receipt touched terminal")
	}
}

func TestNativeOwnerContinuationSupersedesStoppedGenerationQueue(t *testing.T) {
	skipOnWindows(t)
	session, task, _ := remoteGitFixture(t, "stopped-queue-continuation")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	if _, err := StartRemote("claude", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	old := markRemoteExecutorStopped(t, session, task.TaskID)
	request, err := session.Project.CreateRequest(t.Context(), client.NewKey(), task.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: "Old generation only", TargetGeneration: old.Generation})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g2"))
	args := []string{task.TaskID, "--message", "Explicit owner continuation"}
	if code := RunResume(args); code != 0 {
		t.Fatalf("resume exit %d", code)
	}
	next, found, err := session.Credential(task.TaskID)
	if err != nil || !found || next.Generation != old.Generation+1 {
		t.Fatalf("successor=%+v %v", next, err)
	}
	requests, err := session.Project.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].Status != api.RequestSuperseded {
		t.Fatalf("queue=%+v %v", requests, err)
	}
	// Preserve a live local process fact so this is a completed-response retry,
	// not a later intentional continuation after another confirmed stop.
	facts, err := process.ObserveProcess(t.Context(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	boot, err := process.BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	next.AgentPID, next.AgentStart, next.BootID = facts.PID, facts.Start, boot
	if err := remote.WriteCredential(session.Home, next); err != nil {
		t.Fatal(err)
	}
	// The command's completed response can be reread without another handoff.
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	if code := RunResume(args); code != 0 {
		t.Fatalf("completed retry exit %d", code)
	}
	after, _ := os.ReadFile(log)
	if !bytes.Equal(before, after) {
		t.Fatal("completed command physically replayed")
	}
	if _, err := session.Project.Renew(t.Context(), task.TaskID, *old.Claim()); err == nil {
		t.Fatal("old worker retained authority")
	}
}
