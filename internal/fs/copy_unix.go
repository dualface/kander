//go:build unix

package fs

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// CopyRegularFile copies at most limit bytes without following links.
func CopyRegularFile(dst io.Writer, root, path string, limit int64) (int64, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return 0, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, mapOpenErr("read", parent.path, err)
	}
	f := os.NewFile(uintptr(fd), parent.path)
	defer f.Close()
	if err := requireRegular(fd, parent.path); err != nil {
		return 0, err
	}
	return copyFileBytes(dst, f, limit)
}

// CreatePrivateRegularFile opens a new writable 0600 file through a pinned
// parent. It never replaces an entry. The caller owns the returned file.
func CreatePrivateRegularFile(root, path string) (*os.File, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(privateFileMode))
	if err == unix.EEXIST {
		return nil, existError("create", parent.path, "protected file already exists")
	}
	if err != nil {
		return nil, mapOpenErr("create", parent.path, err)
	}
	if err := unix.Fchmod(fd, uint32(privateFileMode)); err != nil {
		unix.Close(fd)
		_ = unix.Unlinkat(parent.parentFD, parent.name, 0)
		return nil, wrap("fchmod", parent.path, err)
	}
	return os.NewFile(uintptr(fd), parent.path), nil
}
