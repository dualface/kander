package launch

import (
	"strings"
	"testing"
)

// promptRecorder wraps the provider of one session mode and records the launch
// facts of every discovery scan it opens; the wrapped provider does the work.
type promptRecorder struct {
	sessionDiscoverer
	seen *[]sessionLaunch
}

func (r promptRecorder) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	*r.seen = append(*r.seen, l)
	return r.sessionDiscoverer.Scan(l, previous)
}

func recordDiscoveryLaunches(t *testing.T, mode string) *[]sessionLaunch {
	t.Helper()
	wrapped, ok := sessionProviders[mode].(sessionDiscoverer)
	if !ok {
		t.Fatalf("%s is not a discovering provider", mode)
	}
	var seen []sessionLaunch
	sessionProviders[mode] = promptRecorder{sessionDiscoverer: wrapped, seen: &seen}
	t.Cleanup(func() { sessionProviders[mode] = wrapped })
	return &seen
}

// stubNewDevinSession makes the second enumeration of a discovered-mode launch
// list one session that was not there before the launch.
func stubNewDevinSession(t *testing.T) {
	t.Helper()
	stubDiscovery(t, func(call int) []string {
		if call == 1 {
			return []string{"existing"}
		}
		return []string{"existing", "devin-new-session"}
	})
}

// requireDiscoveryPrompt checks that the one discovery of a launch saw the
// instruction the launch sent to the agent: it is in the terminal command.
func requireDiscoveryPrompt(t *testing.T, seen []sessionLaunch, command string) {
	t.Helper()
	if len(seen) != 1 {
		t.Fatalf("discovery scans=%d, want 1", len(seen))
	}
	prompt := seen[0].Prompt
	if !strings.Contains(prompt, "task file at ") || !strings.Contains(command, prompt) {
		t.Fatalf("prompt=%q, command=%q", prompt, command)
	}
}

func TestLocalStartHandsTheInstructionToSessionDiscovery(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "discovery-prompt")
	seen := recordDiscoveryLaunches(t, "discovered")
	stubNewDevinSession(t)
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) }); err != nil {
		t.Fatal(err)
	}
	requireDiscoveryPrompt(t, *seen, lastCommand(t, root))
	if (*seen)[0].TaskID != taskID {
		t.Fatalf("task=%q", (*seen)[0].TaskID)
	}
}

func TestRemoteStartHandsTheInstructionToSessionDiscovery(t *testing.T) {
	skipOnWindows(t)
	_, _, task, _, fakeRoot := remoteLifecycleFixture(t, "remote-discovery-prompt")
	seen := recordDiscoveryLaunches(t, "discovered")
	stubNewDevinSession(t)
	if _, err := StartRemote("devin", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	requireDiscoveryPrompt(t, *seen, lastCommand(t, fakeRoot))
}

func TestRemoteOrchestratorHandsTheInstructionToSessionDiscovery(t *testing.T) {
	skipOnWindows(t)
	_, _, task, _, fakeRoot := remoteLifecycleFixture(t, "orchestrate-discovery-prompt")
	seen := recordDiscoveryLaunches(t, "discovered")
	stubNewDevinSession(t)
	request := OrchestrateRequest{References: []string{task.TaskID}, Handover: "Run this confirmed plan once.", Agent: "devin", Launcher: "tmux"}
	if _, err := StartOrchestrator(request); err != nil {
		t.Fatal(err)
	}
	requireDiscoveryPrompt(t, *seen, lastCommand(t, fakeRoot))
}
