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

// killGroup and groupOnlyZombies are variables so tests can inject them.
var (
	killGroup        = syscall.Kill
	groupOnlyZombies = osGroupOnlyZombies
)

func (tree *processTree) kill() error {
	pgid := tree.cmd.Process.Pid
	err := killGroup(-pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if errors.Is(err, syscall.EPERM) {
		// macOS refuses to signal a group whose only members are zombies (or that
		// just lost its last one). Nothing there can run or hold a pipe, so the
		// kill has nothing left to do. Any live member, or a check that cannot
		// finish, keeps the original error.
		if only, checkErr := groupOnlyZombies(pgid); checkErr == nil && only {
			return nil
		}
	}
	return err
}

func (tree *processTree) close() error { return nil }
