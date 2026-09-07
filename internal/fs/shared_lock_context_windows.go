package fs

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func tryContextLock(file *os.File, shared bool) (bool, error) {
	var ov windows.Overlapped
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 0xFFFFFFFF, 0xFFFFFFFF, &ov)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
