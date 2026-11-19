//go:build windows

package fs

import (
	"errors"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsBusyFile reports whether err means the target executable is occupied and cannot be replaced in place.
func IsBusyFile(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION, windows.ERROR_ACCESS_DENIED:
			return true
		}
	}
	return false
}

// IsSharingViolation reports whether err means another open handle blocked this open through its share mode.
// Unlike IsBusyFile it excludes access-denied and lock violations, which can be real refusals.
func IsSharingViolation(err error) bool {
	// errors.Is walks every branch of a joined error, where errors.As would stop at the first errno.
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// RemoveNonDirectoryIfExists unlinks a regular file or reparse point. Directories are rejected.
func RemoveNonDirectoryIfExists(root, path string) (bool, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return false, err
	}
	if len(parts) == 0 {
		return false, failClosed("remove", candidate, "protected path cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return false, err
	}
	defer cleanup()
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindAnyWithReparse, true, false)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if handle == 0 {
		return false, nil
	}
	defer closeHandle(handle)
	info, err := attributeInfo(handle, candidate)
	if err != nil {
		return false, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false, failClosed("remove", candidate, "refusing to remove a directory")
	}
	if err := deleteHandle(handle, candidate, true); err != nil {
		return false, err
	}
	return true, nil
}
