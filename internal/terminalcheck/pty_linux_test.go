//go:build linux

package terminalcheck

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func attachIsolatedClient(t *testing.T, program, socket string) bool {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Logf("focus: PTY unavailable: %v", err)
		return false
	}
	fail := func(err error) bool { master.Close(); t.Logf("focus: PTY unavailable: %v", err); return false }
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return fail(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return fail(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fail(err)
	}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 120}); err != nil {
		slave.Close()
		return fail(err)
	}
	cmd := exec.Command(program, "-S", socket, "attach-session", "-t", "conformance")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "TMUX=")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	if err := cmd.Start(); err != nil {
		slave.Close()
		return fail(err)
	}
	slave.Close()
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, master); close(drained) }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = master.Close()
		select {
		case <-waited:
		case <-time.After(5 * time.Second):
			t.Error("tmux client did not exit")
		}
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Error("PTY reader did not exit")
		}
	})
	return true
}
