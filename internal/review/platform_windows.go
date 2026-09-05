//go:build windows

package review

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func applyUmask() {}

func windowsJobBootstrapMain(arguments []string) int {
	if len(arguments) < 2 {
		return 125
	}
	eventName, reviewer := arguments[0], arguments[1:]
	event, err := windows.OpenEvent(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(eventName))
	if err != nil {
		return 125
	}
	defer windows.CloseHandle(event)
	s, err := windows.WaitForSingleObject(event, windows.INFINITE)
	if err != nil || s != windows.WAIT_OBJECT_0 {
		return 125
	}
	cmd := exec.Command(reviewer[0], reviewer[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 127
	}
	return 0
}
