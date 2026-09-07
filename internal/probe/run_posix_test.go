//go:build !windows

package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func detachProbeHelper(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func helperProcessRunning(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
		// Only the orphan's new parent can reap it. A zombie cannot run or hold pipes.
		if end := strings.LastIndex(string(data), ") "); end >= 0 && len(data) > end+2 {
			return data[end+2] != 'Z' && data[end+2] != 'X'
		}
	}
	return true
}

func TestCaptureClosesPipesHeldOutsideProcessGroup(t *testing.T) {
	configureProbeHelpers(t)
	dir := t.TempDir()
	started := time.Now()
	_, err := Capture(os.Args[0], helperArgs("parent-escape", dir), 300*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 600*time.Millisecond {
		t.Fatalf("pipe wait outlived deadline: %s", elapsed)
	}
	child := helperPID(t, dir, "child")
	// A setsid descendant is outside the process-group ownership boundary. The
	// pipe wait must still finish, and this test explicitly cleans up that child.
	process, findErr := os.FindProcess(child)
	if findErr != nil {
		t.Fatal(findErr)
	}
	defer process.Release()
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
	requireStopped(t, child)
}
