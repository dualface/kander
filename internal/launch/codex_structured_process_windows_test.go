//go:build windows

package launch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/terminal/direct"
	"golang.org/x/sys/windows"
)

func TestLaunchStructuredAgentOwnsWindowsProcessTree(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "descendant.pid")
	invocation := structuredHelperInvocation(t, "tree", pidPath)
	plan := LaunchPlan{
		Launcher:   direct.Foreground,
		Structured: true,
		Prompt:     "prompt",
		sessionFinalize: func(AgentSession) error {
			return errors.New("finalize")
		},
	}
	_, err := launchStructuredAgent(
		plan,
		invocation,
		dir,
		func() (AgentSession, error) { return AgentSession{Agent: "codex"}, nil },
		nil,
	)
	if err == nil {
		t.Fatal("expected finalization failure")
	}
	pid := readStructuredHelperPID(t, pidPath)
	deadline := time.Now().Add(2 * time.Second)
	for processRunningOnWindows(pid) {
		if time.Now().After(deadline) {
			process, findErr := os.FindProcess(pid)
			if findErr == nil {
				_ = process.Kill()
			}
			t.Fatalf("descendant process %d survived structured cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func processRunningOnWindows(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	status, err := windows.WaitForSingleObject(process, 0)
	return err == nil && status == uint32(windows.WAIT_TIMEOUT)
}
