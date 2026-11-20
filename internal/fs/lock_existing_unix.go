//go:build unix

package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// OpenExistingLockFile opens an existing lock file with the access of the
// handle OpenLockFile returns, and never creates it. A missing file or parent
// directory returns (nil, nil).
func OpenExistingLockFile(root, path string) (*os.File, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return nil, mapOpenErr("lock-open", path, err)
	}
	if err = requireRegular(fd, path); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
