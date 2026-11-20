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
	// The deadline must outlast the startup of both helpers under load, so the
	// child has published its PID file before it expires. The upper bound stays
	// below the child's five-second lifetime, so waiting for the child to exit
	// still fails this test.
	const deadline = 2 * time.Second
	const cleanupTolerance = 1500 * time.Millisecond
	dir := t.TempDir()
	started := time.Now()
	_, err := captureWithin(os.Args[0], helperArgs("parent-escape", dir), deadline)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > deadline+cleanupTolerance {
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
