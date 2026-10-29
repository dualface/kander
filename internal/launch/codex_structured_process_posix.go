//go:build !windows

package launch

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type structuredProcessTree struct {
	cmd *exec.Cmd
}

func newStructuredProcessTree(cmd *exec.Cmd) (*structuredProcessTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &structuredProcessTree{cmd: cmd}, nil
}

func (tree *structuredProcessTree) start() error {
	return tree.cmd.Start()
}

func (tree *structuredProcessTree) signal(force bool) error {
	if tree == nil || tree.cmd == nil || tree.cmd.Process == nil {
		return nil
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	err := syscall.Kill(-tree.cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (tree *structuredProcessTree) close() error { return nil }

func watchStructuredSignals(tree *structuredProcessTree, done <-chan struct{}) func() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	stopForwarding := forwardStructuredSignals(tree, done, signals)
	return func() {
		signal.Stop(signals)
		stopForwarding()
	}
}

func forwardStructuredSignals(tree *structuredProcessTree, done <-chan struct{}, signals <-chan os.Signal) func() {
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		var escalation *time.Timer
		var escalationC <-chan time.Time
		defer func() {
			if escalation != nil {
				escalation.Stop()
			}
		}()
		for {
			select {
			case <-done:
				return
			case <-stop:
				return
			case <-escalationC:
				_ = tree.signal(true)
				return
			case <-signals:
				if escalation == nil {
					_ = tree.signal(false)
					escalation = time.NewTimer(structuredCleanupGrace)
					escalationC = escalation.C
					continue
				}
				_ = tree.signal(true)
				return
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}
