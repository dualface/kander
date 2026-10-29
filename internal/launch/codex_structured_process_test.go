package launch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal/direct"
)

func TestLaunchStructuredAgentWritesPromptAndCapturesSession(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\ncat > " + posixQuote(input) + "\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	plan := LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "read the task file"}
	invocation, err := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := AgentSession{Agent: "codex"}
	outcome, err := launchAgent(plan, dir, "structured", invocation, nil, func() (AgentSession, error) { return session, nil }, &session)
	if err != nil {
		t.Fatal(err)
	}
	if session.Reference != "thread-123" || outcome.Session != "thread-123" {
		t.Fatalf("session = %+v, outcome = %+v", session, outcome)
	}
	code, err := outcome.Wait()
	if err != nil || code != 0 {
		t.Fatalf("wait = %d, %v", code, err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(input)
		if err == nil {
			if string(data) != plan.Prompt {
				t.Fatalf("stdin = %q, want %q", data, plan.Prompt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}
