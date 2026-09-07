//go:build !windows

package probe

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

type processTree struct{ cmd *exec.Cmd }

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{cmd: cmd}, nil
}

func (tree *processTree) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return tree.cmd.Start()
}

func (tree *processTree) kill() error {
	err := syscall.Kill(-tree.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (tree *processTree) close() error { return nil }
