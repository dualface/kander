//go:build windows

package launch

import "os/exec"

func configureStructuredProcess(cmd *exec.Cmd) {}

func attachStructuredProcess(*exec.Cmd) error { return nil }

func releaseStructuredProcess(*exec.Cmd) {}

func watchStructuredSignals(*exec.Cmd, <-chan struct{}) func() { return func() {} }

func signalStructuredProcess(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
