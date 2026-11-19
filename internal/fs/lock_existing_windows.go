//go:build windows

package fs

import (
	"os"

	"golang.org/x/sys/windows"
)

// OpenExistingLockFile opens an existing lock file with the access and sharing
// of the handle OpenLockFile returns, and never creates it. A missing file or
// parent directory returns (nil, nil).
func OpenExistingLockFile(root, path string) (*os.File, error) {
	return openExistingRegular(root, path, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES, true)
}
