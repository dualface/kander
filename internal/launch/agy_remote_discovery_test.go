package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/remote"
)

const (
	agyLaunchedConversation = "agy-conversation-of-this-launch"
	agyOtherConversation    = "agy-conversation-of-another-launch"
)

// agyConversationWriter stands in for agy. When discovery opens its scan after
// the launch, it writes the conversation agy would have created for the
// instruction the launch sent, and a conversation of another launch whose first
// request only starts with that instruction. The wrapped provider does the
// real work, so the id it binds proves that the launch handed it the exact
// instruction.
type agyConversationWriter struct {
	sessionDiscoverer
	t       *testing.T
	root    string
	prompts *[]string
}

func (w agyConversationWriter) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	*w.prompts = append(*w.prompts, l.Prompt)
	writeAgyTranscript(w.t, w.root, agyOtherConversation, agyTranscriptLine(l.Prompt+" and more"))
	writeAgyTranscript(w.t, w.root, agyLaunchedConversation, agyTranscriptLine(l.Prompt))
	return w.sessionDiscoverer.Scan(l, previous)
}

// fakeAgyConversations makes the next agy launch create its conversation, and
// returns the instructions the discovery scans received.
func fakeAgyConversations(t *testing.T) *[]string {
	t.Helper()
	const mode = "hook:agy-transcript"
	wrapped, ok := sessionProviders[mode].(sessionDiscoverer)
	if !ok {
		t.Fatalf("%s is not a discovering provider", mode)
	}
	frozenDiscoveryClock(t)
	var prompts []string
	sessionProviders[mode] = agyConversationWriter{sessionDiscoverer: wrapped, t: t, root: agyBrainDir(t), prompts: &prompts}
	t.Cleanup(func() { sessionProviders[mode] = wrapped })
	return &prompts
}

func requireAgyLaunchInstruction(t *testing.T, prompts []string, command string) {
	t.Helper()
	if len(prompts) != 1 {
		t.Fatalf("discovery scans=%d, want 1", len(prompts))
	}
	if !strings.Contains(prompts[0], "task file at ") || !strings.Contains(command, prompts[0]) {
		t.Fatalf("prompt=%q, command=%q", prompts[0], command)
	}
}

func TestRemoteStartRecordsTheAgyConversationOfTheLaunchInstruction(t *testing.T) {
	skipOnWindows(t)
	_, session, task, _, fakeRoot := remoteLifecycleFixture(t, "remote-agy-conversation")
	prompts := fakeAgyConversations(t)
	if _, err := StartRemote("agy", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	requireAgyLaunchInstruction(t, *prompts, lastCommand(t, fakeRoot))
	record, err := session.Project.ExecutionRecord(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Session != "agy "+agyLaunchedConversation {
		t.Fatalf("session=%q, want the conversation of this launch", record.Session)
	}
}

func TestRemoteOrchestratorRecordsTheAgyConversationOfTheLaunchInstruction(t *testing.T) {
	skipOnWindows(t)
	_, _, task, _, fakeRoot := remoteLifecycleFixture(t, "orchestrate-agy-conversation")
	prompts := fakeAgyConversations(t)
	request := OrchestrateRequest{References: []string{task.TaskID}, Handover: "Run this confirmed plan once.", Agent: "agy", Launcher: "tmux"}
	if _, err := StartOrchestrator(request); err != nil {
		t.Fatal(err)
	}
	requireAgyLaunchInstruction(t, *prompts, lastCommand(t, fakeRoot))
	files, err := filepath.Glob(filepath.Join(os.Getenv(remote.EnvClientHome), "sessions", "*", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("private session context: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"session": "agy `+agyLaunchedConversation+`"`) && !strings.Contains(string(data), `"session":"agy `+agyLaunchedConversation+`"`) {
		t.Fatalf("private session context does not name the conversation of this launch: %s", data)
	}
}
