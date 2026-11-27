package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/remote"
)

const missingConfigDirMessage = "agent configuration directory is missing; using the scope configuration"

func TestWorkerHandsTheRecordedConfigurationDirectoryToItsHooks(t *testing.T) {
	h := newHarness(t)
	const window, session = "herdr:w1:t9:w1:p9", "codex id-1"
	_, c := h.claimedWith("config-dir-hooks", "exec-a", &api.StartInput{Window: window, Session: session})
	project := t.TempDir()
	c.ProjectDir = project
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	h.request(c, "look at this")
	pr := h.target(nil)
	var resolved []AgentAddress
	h.loop.o.Resolve = func(address AgentAddress) (*Target, error) {
		resolved = append(resolved, address)
		return pr.resolve(address)
	}
	h.workAfter(16 * time.Second)
	if want := (AgentAddress{Window: window, Session: session, ConfigDir: project}); len(resolved) == 0 || resolved[0] != want {
		t.Fatalf("resolved %+v, want %+v", resolved, want)
	}
	var stopped []AgentAddress
	previous := StopAgent
	StopAgent = func(_ context.Context, address AgentAddress) error {
		stopped = append(stopped, address)
		return nil
	}
	t.Cleanup(func() { StopAgent = previous })
	var observed []AgentAddress
	previousObserve := ObserveAgent
	ObserveAgent = func(_ context.Context, address AgentAddress) api.AgentObservation {
		observed = append(observed, address)
		return api.AgentObservation{Status: "alive"}
	}
	t.Cleanup(func() { ObserveAgent = previousObserve })
	c.AgentPID = 0
	h.loop.observe(t.Context(), c)
	h.loop.stopLost(t.Context(), h.loop.o.Logger, c)
	want := AgentAddress{Window: window, Session: session, ConfigDir: project}
	if len(observed) != 1 || observed[0] != want {
		t.Fatalf("observed %+v, want %+v", observed, want)
	}
	if len(stopped) != 1 || stopped[0] != want {
		t.Fatalf("stopped %+v, want %+v", stopped, want)
	}
}

func TestWorkerLogsAMissingConfigurationDirectoryOnce(t *testing.T) {
	h := newHarness(t)
	sink := h.logs()
	existing := t.TempDir()
	missing := filepath.Join(existing, "removed")
	c := remote.Credential{ProjectID: "project", TaskID: "20261127-config-dir-task", ExecutionID: "exec-a", ProjectDir: missing, Worktree: existing}
	for range 2 {
		if got := h.loop.agentConfigDir(c); got != "" {
			t.Fatalf("a missing directory was handed to the hooks: %q", got)
		}
	}
	records := sink.findAll(missingConfigDirMessage)
	if len(records) != 1 || records[0]["level"] != "WARN" || records[0]["task"] != c.TaskID || records[0]["dir"] != missing {
		t.Fatalf("missing directory log = %v", records)
	}
	worktreeOnly := c
	worktreeOnly.ProjectDir = ""
	if got := h.loop.agentConfigDir(worktreeOnly); got != existing {
		t.Fatalf("worktree fallback = %q, want %q", got, existing)
	}
	if records := sink.findAll(missingConfigDirMessage); len(records) != 1 {
		t.Fatalf("an existing directory was logged: %v", records)
	}
}
