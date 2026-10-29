//go:build !windows

package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal/direct"
)

func TestStructuredSignalForwardingStopsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\nsleep 30 &\necho $! > " + posixQuote(childPID) + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script)
	configureStructuredProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	signals := make(chan os.Signal, 1)
	stop := forwardStructuredSignals(cmd, done, signals)
	defer stop()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(childPID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child pid was not written")
		}
		time.Sleep(time.Millisecond)
	}
	signals <- syscall.SIGINT
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
		close(done)
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("forwarded interrupt did not stop the process group")
	}
}

func TestLaunchStructuredAgentCleansProcessTreeWhenFinalizationFails(t *testing.T) {
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\ncat >/dev/null\nsleep 30 &\necho $! > " + posixQuote(childPID) + "\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-cleanup\"}'\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation, _ := process.NewProcessInvocation(process.AgentProgram{Path: script}, []string{"exec", "--json", "-"}, nil)
	plan := LaunchPlan{Launcher: direct.Foreground, Structured: true, Prompt: "prompt", sessionFinalize: func(AgentSession) error { return errors.New("finalize") }}
	_, err := launchStructuredAgent(plan, invocation, dir, func() (AgentSession, error) { return AgentSession{Agent: "codex"}, nil }, nil)
	if err == nil {
		t.Fatal("expected finalization failure")
	}
	data, readErr := os.ReadFile(childPID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var pid int
	if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); scanErr != nil {
		t.Fatal(scanErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		probeErr := syscall.Kill(pid, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived cleanup: %v", pid, probeErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
