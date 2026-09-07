//go:build windows

package probe

import (
	"golang.org/x/sys/windows"
	"os/exec"
)

func detachProbeHelper(cmd *exec.Cmd) {}

func helperProcessRunning(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err != windows.ERROR_INVALID_PARAMETER
	}
	defer windows.CloseHandle(process)
	status, err := windows.WaitForSingleObject(process, 0)
	return err != nil || status == uint32(windows.WAIT_TIMEOUT)
}
