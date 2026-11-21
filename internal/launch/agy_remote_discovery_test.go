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
// instruction the launch command passed to --prompt-interactive, and a
// conversation of another launch whose first request only starts with that
// instruction. The request is read from the launch command, never from the
// facts discovery received, so the wrapped provider binds the conversation only
// when it was handed the same, complete instruction.
type agyConversationWriter struct {
	sessionDiscoverer
	t       *testing.T
	root    string
	command func() string
	scans   *int
}

func (w agyConversationWriter) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	*w.scans++
	request := launchedAgyRequest(w.t, w.command())
	writeAgyTranscript(w.t, w.root, agyOtherConversation, agyTranscriptLine(request+" and more"))
	writeAgyTranscript(w.t, w.root, agyLaunchedConversation, agyTranscriptLine(request))
	return w.sessionDiscoverer.Scan(l, previous)
}

// launchedAgyRequest returns the instruction a launch command passes to agy as
// the value of --prompt-interactive.
func launchedAgyRequest(t *testing.T, command string) string {
	t.Helper()
	const flag = " --prompt-interactive '"
	i := strings.LastIndex(command, flag)
	if i < 0 || !strings.HasSuffix(command, "'\n") {
		t.Fatalf("the launch command does not end with a quoted --prompt-interactive value: %q", command)
	}
	request := command[i+len(flag) : len(command)-2]
	if !strings.Contains(request, "task file at ") || !strings.HasSuffix(request, "follow it exactly.") {
		t.Fatalf("request=%q is not the full launch instruction", request)
	}
	return request
}

// fakeAgyConversations makes the next agy launch create its conversation from
// the command the launch sent to the terminal, which command returns, and
// counts the discovery scans.
func fakeAgyConversations(t *testing.T, command func() string) *int {
	t.Helper()
	const mode = "hook:agy-transcript"
	wrapped, ok := sessionProviders[mode].(sessionDiscoverer)
	if !ok {
		t.Fatalf("%s is not a discovering provider", mode)
	}
	frozenDiscoveryClock(t)
	scans := new(int)
	sessionProviders[mode] = agyConversationWriter{sessionDiscoverer: wrapped, t: t, root: agyBrainDir(t), command: command, scans: scans}
	t.Cleanup(func() { sessionProviders[mode] = wrapped })
	return scans
}

func requireOneAgyScan(t *testing.T, scans *int) {
	t.Helper()
	if *scans != 1 {
		t.Fatalf("discovery scans=%d, want 1", *scans)
	}
}

func TestRemoteStartRecordsTheAgyConversationOfTheLaunchInstruction(t *testing.T) {
	skipOnWindows(t)
	_, session, task, _, fakeRoot := remoteLifecycleFixture(t, "remote-agy-conversation")
	scans := fakeAgyConversations(t, func() string { return lastCommand(t, fakeRoot) })
	if _, err := StartRemote("agy", "tmux", task.TaskID); err != nil {
		t.Fatal(err)
	}
	requireOneAgyScan(t, scans)
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
	scans := fakeAgyConversations(t, func() string { return lastCommand(t, fakeRoot) })
	request := OrchestrateRequest{References: []string{task.TaskID}, Handover: "Run this confirmed plan once.", Agent: "agy", Launcher: "tmux"}
	if _, err := StartOrchestrator(request); err != nil {
		t.Fatal(err)
	}
	requireOneAgyScan(t, scans)
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
