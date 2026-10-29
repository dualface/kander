package launch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal/direct"
)

const (
	structuredHelperMode = "KANDER_STRUCTURED_HELPER_MODE"
	structuredHelperPath = "KANDER_STRUCTURED_HELPER_PATH"
)

type deadlineUnavailablePromptWriter struct {
	strings.Builder
	closed bool
}

func (writer *deadlineUnavailablePromptWriter) Close() error {
	writer.closed = true
	return nil
}

func (*deadlineUnavailablePromptWriter) SetWriteDeadline(time.Time) error {
	return errors.New("file type does not support deadline")
}

func TestWriteStructuredPromptDoesNotRequireDeadlineSupport(t *testing.T) {
	writer := &deadlineUnavailablePromptWriter{}
	written, err := writeStructuredPrompt(writer, writer.Close, func() error { return nil }, "prompt", time.Second)
	if err != nil {
		t.Fatalf("write structured prompt: %v", err)
	}
	if written != len("prompt") || writer.String() != "prompt" || !writer.closed {
		t.Fatalf("write = (%d, %q, closed %v)", written, writer.String(), writer.closed)
	}
}

type blockedPromptWriter struct {
	started  chan struct{}
	canceled chan struct{}
	finished chan struct{}
}

func (writer *blockedPromptWriter) Write([]byte) (int, error) {
	close(writer.started)
	<-writer.canceled
	close(writer.finished)
	return 3, errors.New("write canceled")
}

func (writer *blockedPromptWriter) Close() error {
	<-writer.finished
	return nil
}

func TestWriteStructuredPromptCancelsAndJoinsBlockedWrite(t *testing.T) {
	writer := &blockedPromptWriter{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		finished: make(chan struct{}),
	}
	written, err := writeStructuredPrompt(
		writer,
		writer.Close,
		func() error {
			<-writer.started
			close(writer.canceled)
			return nil
		},
		"prompt",
		10*time.Millisecond,
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if written != 3 {
		t.Fatalf("written = %d, want partial count 3", written)
	}
}

func TestStructuredProcessHelper(t *testing.T) {
	mode := os.Getenv(structuredHelperMode)
	if mode == "" {
		return
	}
	path := os.Getenv(structuredHelperPath)
	switch mode {
	case "blocked-stdin":
		writeStructuredHelperPID(path)
		time.Sleep(30 * time.Second)
	case "stdin":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"thread-stdin"}`)
	case "descendant":
		writeStructuredHelperPID(path)
		time.Sleep(30 * time.Second)
	case "exit-with-descendant":
		_, _ = io.Copy(io.Discard, os.Stdin)
		child := exec.Command(os.Args[0], "-test.run=^TestStructuredProcessHelper$")
		child.Env = structuredHelperEnv("descendant", path)
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		waitForStructuredHelperPath(path)
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"thread-descendant"}`)
	case "stderr":
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Fprintln(os.Stderr, "structured helper diagnostic")
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"thread-stderr"}`)
	case "tree":
		_, _ = io.Copy(io.Discard, os.Stdin)
		child := exec.Command(os.Args[0], "-test.run=^TestStructuredProcessHelper$")
		child.Env = structuredHelperEnv("descendant", path)
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		waitForStructuredHelperPath(path)
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"thread-tree"}`)
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown structured helper mode")
		os.Exit(2)
	}
	os.Exit(0)
}

func structuredHelperEnv(mode, path string) []string {
	env := append([]string{}, os.Environ()...)
	return append(env, structuredHelperMode+"="+mode, structuredHelperPath+"="+path)
}

func structuredHelperInvocation(t *testing.T, mode, path string) process.ProcessInvocation {
	t.Helper()
	env := make(map[string]string)
	for _, item := range structuredHelperEnv(mode, path) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	invocation, err := process.NewProcessInvocation(
		process.AgentProgram{Path: os.Args[0]},
		[]string{"-test.run=^TestStructuredProcessHelper$"},
		env,
	)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func writeStructuredHelperPID(path string) {
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func waitForStructuredHelperPath(path string) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "structured helper child did not start")
			os.Exit(2)
		}
		time.Sleep(time.Millisecond)
	}
}

func readStructuredHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatal(convErr)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}

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

func TestLaunchStructuredAgentWritesSmallPromptPortable(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	invocation := structuredHelperInvocation(t, "stdin", input)
	session := AgentSession{Agent: "codex"}
	outcome, err := launchStructuredAgent(
		LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "portable prompt"},
		invocation,
		dir,
		func() (AgentSession, error) { return session, nil },
		&session,
	)
	if err != nil {
		t.Fatal(err)
	}
	code, err := outcome.Wait()
	if err != nil || code != 0 {
		t.Fatalf("wait = %d, %v", code, err)
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "portable prompt" || session.Reference != "thread-stdin" {
		t.Fatalf("stdin = %q, session = %+v", data, session)
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

func TestLaunchStructuredAgentDoesNotWriteChildStderrToGlobalStream(t *testing.T) {
	dir := t.TempDir()
	invocation := structuredHelperInvocation(t, "stderr", filepath.Join(dir, "unused"))
	session := AgentSession{Agent: "codex"}
	_, stderr, runErr := capture(t, func() error {
		outcome, err := launchStructuredAgent(
			LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"},
			invocation,
			dir,
			func() (AgentSession, error) { return session, nil },
			&session,
		)
		if err != nil {
			return err
		}
		_, err = outcome.Wait()
		return err
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if session.Reference != "thread-stderr" {
		t.Fatalf("session = %+v", session)
	}
	if strings.Contains(stderr, "structured helper diagnostic") {
		t.Fatalf("child stderr leaked to global stream: %q", stderr)
	}
}

func TestLaunchStructuredAgentBoundsBlockedStdinWrite(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "blocked.pid")
	invocation := structuredHelperInvocation(t, "blocked-stdin", pidPath)
	result := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := launchStructuredAgent(
			LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: strings.Repeat("x", 1024*1024)},
			invocation,
			dir,
			nil,
			nil,
			true,
		)
		result <- err
	}()
	pid := readStructuredHelperPID(t, pidPath)
	select {
	case err := <-result:
		failure := asLaunchFailure(err)
		if err == nil || failure == nil || !failure.DeliveryUnknown {
			t.Fatalf("blocked stdin delivery = %#v, want uncertain failure", failure)
		}
	case <-time.After(4 * structuredCleanupGrace):
		process, _ := os.FindProcess(pid)
		_ = process.Kill()
		<-result
		t.Fatalf("blocked stdin write exceeded bound: %s", time.Since(started))
	}
}

func TestLaunchStructuredAgentBoundsDrainHeldByDescendant(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "descendant.pid")
	invocation := structuredHelperInvocation(t, "exit-with-descendant", pidPath)
	session := AgentSession{Agent: "codex"}
	outcome, err := launchStructuredAgent(
		LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt"},
		invocation,
		dir,
		func() (AgentSession, error) { return session, nil },
		&session,
	)
	if err != nil {
		t.Fatal(err)
	}
	pid := readStructuredHelperPID(t, pidPath)
	waited := make(chan error, 1)
	go func() {
		_, err := outcome.Wait()
		waited <- err
	}()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * structuredCleanupGrace):
		process, _ := os.FindProcess(pid)
		_ = process.Kill()
		<-waited
		t.Fatal("structured output drain remained blocked by a descendant")
	}
	process, _ := os.FindProcess(pid)
	_ = process.Kill()
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
