//go:build windows

package notify

import (
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func notificationTempRoot() (string, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GetTempPathW")
	const capacity = 32768
	buf := make([]uint16, capacity)
	n, _, err := proc.Call(uintptr(capacity), uintptr(unsafe.Pointer(&buf[0])))
	length := uint32(n)
	if length == 0 || length >= capacity {
		detail := "invalid temporary path"
		if err != nil && err != syscall.Errno(0) {
			detail = err.Error()
		}
		return "", notifyError(
			"notify.could_not_determine_the_windows_temporary_directory", detail,
		)
	}
	return filepath.Clean(windows.UTF16ToString(buf[:length])), nil
}
