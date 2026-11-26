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

	"github.com/dualface/kander/v2/internal/config"
)

func TestRemoteLostClaimResponseRecoversOriginalKeyWithoutLaunching(t *testing.T) {
	skipOnWindows(t)
	cfg, direct, task, forbidden, _ := remoteLifecycleFixture(t, "lost-launch-claim")
	originalURL := cfg.Server.URL
	var hide atomic.Bool
	hide.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := http.NewRequestWithContext(r.Context(), r.Method, originalURL+r.URL.RequestURI(), r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		request.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer response.Body.Close()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/claim") && hide.Load() {
			_, _ = io.Copy(io.Discard, response.Body)
			http.Error(w, "hidden committed response", http.StatusServiceUnavailable)
			return
		}
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	cfg.Server.URL = proxy.URL
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	if _, err := StartRemote("claude", "tmux", task.TaskID); err == nil {
		t.Fatal("hidden claim response appeared successful")
	}
	claimed, err := direct.Project.Show(t.Context(), task.TaskID)
	if err != nil || claimed.Claim == nil || claimed.Claim.Generation != 1 {
		t.Fatalf("claim not committed: %+v %v", claimed, err)
	}
	execution := claimed.Claim.ExecutionID
	hide.Store(false)
	// A new invocation opens the private journal before enforcing todo-only start.
	if _, err := StartRemote("claude", "tmux", task.TaskID); err == nil {
		t.Fatal("recovery pretended it launched an executor")
	}
	recovered, err := direct.Project.Show(t.Context(), task.TaskID)
	if err != nil || recovered.State != "todo" || recovered.Claim.ExecutionID != execution || recovered.Claim.Status != "released" {
		t.Fatalf("claim recovery: %+v %v", recovered, err)
	}
	after, _ := os.ReadFile(log)
	if !bytes.Equal(before, after) {
		t.Fatal("claim-response recovery invoked a terminal")
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("local board used: %v", err)
	}
}

func TestRemoteOrchestratorUncertainLaunchIsNeverRepeated(t *testing.T) {
	skipOnWindows(t)
	_, service, task, _, fakeRoot := remoteLifecycleFixture(t, "orchestrate-action-recovery")
	request := OrchestrateRequest{References: []string{task.TaskID}, Handover: "Run this confirmed plan once.", Agent: "devin", Launcher: "tmux"}
	lateDiscoveryFailure(t, func() {})
	if _, err := StartOrchestrator(request); err == nil {
		t.Fatal("late discovery failure accepted")
	}
	if containerClosed(fakeRoot) {
		t.Fatal("uncertain orchestrator was closed")
	}
	log := os.Getenv("KANBAN_TMUX_LOG")
	first, _ := os.ReadFile(log)
	if _, err := StartOrchestrator(request); err == nil {
		t.Fatal("uncertain launch was replayed")
	}
	second, _ := os.ReadFile(log)
	if !bytes.Equal(first, second) {
		t.Fatal("retry touched the terminal")
	}
	journal, fresh, err := service.BeginAction("orchestrate", struct {
		References                             []string
		Handover, Agent, Launcher, OperationID string
	}{request.References, request.Handover, request.Agent, request.Launcher, request.OperationID})
	if err != nil || fresh {
		t.Fatalf("missing durable action: %v", err)
	}
	defer journal.Close()
	if (journal.Value.Phase != "prompt-attempted" && journal.Value.Phase != "launching") || journal.Value.Window == "" || journal.Value.TaskFile == "" {
		t.Fatalf("lost recovery facts: %+v", journal.Value)
	}
	if _, err := os.Stat(journal.Value.TaskFile); err != nil {
		t.Fatalf("uncertain launch lost task file: %v", err)
	}
}

// A missing allocator refuses the start before the receipt records the
// allocation, so the same start reaches the allocator once it is installed.
func TestRemoteMissingSessionAllocatorKeepsStartRetryable(t *testing.T) {
	skipOnWindows(t)
	cfg, service, task, _, fakeRoot := remoteLifecycleFixture(t, "missing-session-allocator")
	allocator := filepath.Join(fakeRoot, "allocator")
	cfg.Agents = map[string]config.AgentDefinition{"custom": {
		Path:    filepath.Join(fakeRoot, "fake-bin", "codex"),
		Dialect: "codex",
		Session: &config.AgentSessionDefinition{Mode: "allocated", Allocate: []string{allocator}},
	}}
	if _, err := StartRemote("custom", "tmux", task.TaskID); err == nil || err.Error() != config.Text("launch.agent_is_not_in_path", allocator) {
		t.Fatalf("missing allocator: %v", err)
	}
	marker := filepath.Join(fakeRoot, "allocator-ran")
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(allocator, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRemote("custom", "tmux", task.TaskID); err == nil || err.Error() != config.Text("launch.agent_allocate_failed", 1) {
		t.Fatalf("retry after installing the allocator: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("retry did not reach the allocator: %v", err)
	}
	current, err := service.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.State != "todo" || current.Claim != nil {
		t.Fatalf("refused start mutated the card: %+v %v", current, err)
	}
}

func TestRemoteUncertainSessionAllocationIsNeverRepeated(t *testing.T) {
	skipOnWindows(t)
	_, service, task, _, fakeRoot := remoteLifecycleFixture(t, "session-allocation-recovery")
	log := filepath.Join(fakeRoot, "allocation-count")
	t.Setenv("KANDER_ALLOCATION_LOG", log)
	script := `#!/bin/sh
if [ "$1" = "create-chat" ]; then
 printf 'allocated\n' >> "$KANDER_ALLOCATION_LOG"
 printf 'allocation may have committed\n' >&2
 exit 1
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(fakeRoot, "fake-bin", "cursor-agent"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := StartRemote("cursor", "tmux", task.TaskID); err == nil {
			t.Fatal("uncertain allocation succeeded")
		}
	}
	data, err := os.ReadFile(log)
	if err != nil || string(data) != "allocated\n" {
		t.Fatalf("allocation was repeated: %q %v", data, err)
	}
	current, err := service.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.State != "todo" || current.Claim != nil {
		t.Fatalf("allocation mutated the card: %+v %v", current, err)
	}
}
