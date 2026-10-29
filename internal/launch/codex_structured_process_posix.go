//go:build !windows

package launch

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

func configureStructuredProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachStructuredProcess(*exec.Cmd) error { return nil }

func releaseStructuredProcess(*exec.Cmd) {}

func watchStructuredSignals(cmd *exec.Cmd, done <-chan struct{}) func() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	stopForwarding := forwardStructuredSignals(cmd, done, signals)
	return func() {
		signal.Stop(signals)
		stopForwarding()
	}
}

func forwardStructuredSignals(cmd *exec.Cmd, done <-chan struct{}, signals <-chan os.Signal) func() {
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-done:
				return
			case <-stop:
				return
			case <-signals:
				_ = signalStructuredProcess(cmd, false)
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}

func signalStructuredProcess(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
