//go:build windows

package install

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetCurrentPackageFullName = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentPackageFullName")

var detectPackaged = sync.OnceValue(func() bool {
	if procGetCurrentPackageFullName.Find() != nil {
		return false
	}
	var length uint32
	result, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
	// A zero-length probe returns ERROR_INSUFFICIENT_BUFFER when the process
	// has package identity and APPMODEL_ERROR_NO_PACKAGE when it does not.
	return errors.Is(windows.Errno(result), windows.ERROR_INSUFFICIENT_BUFFER)
})
