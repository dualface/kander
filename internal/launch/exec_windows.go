//go:build windows

package launch

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func applyConsoleAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    false,
		CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
