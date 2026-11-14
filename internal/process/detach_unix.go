//go:build unix

package process

import (
	"os/exec"
	"syscall"
)

// Detach configures a child to outlive the launching terminal.
func Detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
