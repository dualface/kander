//go:build windows

package fs

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsSharingViolationMatchesOnlyTheShareModeConflict(t *testing.T) {
	violation := &pathError{Op: "open", Path: "x", Err: windows.ERROR_SHARING_VIOLATION}
	for name, err := range map[string]error{
		"errno":      windows.ERROR_SHARING_VIOLATION,
		"path error": violation,
		"os path":    &os.PathError{Op: "open", Path: "x", Err: windows.ERROR_SHARING_VIOLATION},
		"wrapped":    fmt.Errorf("read: %w", violation),
		"joined":     errors.Join(errors.New("other"), violation),
		"nt status":  wrapNTStatus(windows.STATUS_SHARING_VIOLATION),
	} {
		if !IsSharingViolation(err) {
			t.Errorf("%s: want a sharing violation", name)
		}
	}
	for name, err := range map[string]error{
		"nil":       nil,
		"plain":     errors.New("busy"),
		"access":    &pathError{Op: "open", Path: "x", Err: windows.ERROR_ACCESS_DENIED},
		"lock":      &pathError{Op: "open", Path: "x", Err: windows.ERROR_LOCK_VIOLATION},
		"missing":   &pathError{Op: "open", Path: "x", Err: windows.ERROR_FILE_NOT_FOUND},
		"not exist": os.ErrNotExist,
		"unsafe":    failClosed("open", "x", "reparse point is not allowed"),
	} {
		if IsSharingViolation(err) {
			t.Errorf("%s: must not be a sharing violation", name)
		}
	}
}

// wrapNTStatus maps st the way openRelative does for a failed NT open.
func wrapNTStatus(st windows.NTStatus) error {
	return mapNTError("open", "x", st)
}
