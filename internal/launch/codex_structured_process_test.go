package launch

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLaunchStructuredAgentDrainsLargeJSONLWithoutWritingGlobalStdout(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	large := strings.Repeat("x", 96*1024)
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' " + posixQuote(`{"type":"item.completed","payload":"`+large+`"}`) + "\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-large\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, err := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := AgentSession{Agent: "codex"}
	stdout, _, runErr := capture(t, func() error {
		outcome, err := launchAgent(LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"}, dir, "structured", invocation, nil, func() (AgentSession, error) { return session, nil }, &session)
		if err != nil {
			return err
		}
		_, err = outcome.Wait()
		return err
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if session.Reference != "thread-large" {
		t.Fatalf("session = %+v", session)
	}
	if stdout != "" {
		t.Fatalf("JSONL leaked to stdout: %.200q", stdout)
	}
}

func TestLaunchStructuredAgentRejectsOversizeJSONLRecord(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"type\":\"item.completed\",\"payload\":\"'\ndd if=/dev/zero bs=1048576 count=9 2>/dev/null | tr '\\0' x\nprintf '%s\\n' '\"}'\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-oversize\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	session := AgentSession{Agent: "codex"}
	_, err := launchAgent(LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"}, dir, "structured", invocation, nil, func() (AgentSession, error) { return session, nil }, &session)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchStructuredAgentReportsMalformedJSONL(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{not-json}'\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-json\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	session := AgentSession{Agent: "codex"}
	if _, err := launchAgent(LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"}, dir, "structured", invocation, nil, func() (AgentSession, error) { return session, nil }, &session); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchStructuredAgentTimesOutWaitingForThreadStarted(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nsleep 12\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	started := time.Now()
	_, err := launchStructuredAgent(LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"}, invocation, dir, func() (AgentSession, error) { return AgentSession{Agent: "codex"}, nil }, nil)
	if err == nil || time.Since(started) >= 11*time.Second {
		t.Fatalf("err = %v, elapsed = %v", err, time.Since(started))
	}
}

func TestStructuredPartialWriteIsDeliveryUncertain(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ndd bs=1 count=1 of=/dev/null 2>/dev/null\nexec 0<&-\nsleep 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, nil, nil)
	_, err := launchStructuredAgent(LaunchPlan{Structured: true, Prompt: strings.Repeat("x", 1024*1024)}, invocation, dir, nil, nil, true)
	failure := asLaunchFailure(err)
	if err == nil || failure == nil || !failure.DeliveryUnknown {
		t.Fatalf("partial stdin delivery must be uncertain, got %#v", failure)
	}
}

func TestStructuredPostWriteFailureIsDeliveryUncertainForDurableResume(t *testing.T) {
	if runtimeWindows() {
		t.Skip("shell fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{not-json}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "resume", "--json", "thread", "-"}, nil)
	_, err := launchAgent(LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"}, filepath.Join(dir, "board"), "structured", invocation, nil, func() (AgentSession, error) { return AgentSession{Agent: "codex"}, nil }, nil, true)
	failure := asLaunchFailure(err)
	if err == nil || failure == nil || !failure.DeliveryUnknown {
		unknown := false
		if failure != nil {
			unknown = failure.DeliveryUnknown
		}
		t.Fatalf("err = %v, delivery unknown = %v", err, unknown)
	}
}
