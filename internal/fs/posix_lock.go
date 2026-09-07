//go:build unix

package fs

import (
	"os"

	"golang.org/x/sys/unix"
)

// LockExclusive takes a blocking cross-process exclusive lock on the whole file.
func LockExclusive(file *os.File) (*ExclusiveLock, error) {
	if file == nil {
		return nil, wrap("lock", "", os.ErrInvalid)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return nil, wrap("lock", file.Name(), err)
	}
	return &ExclusiveLock{file: file}, nil
}

// LockShared takes a blocking cross-process shared lock on the whole file.
func LockShared(file *os.File) (*ExclusiveLock, error) {
	if file == nil {
		return nil, wrap("lock", "", os.ErrInvalid)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_SH); err != nil {
		return nil, wrap("lock", file.Name(), err)
	}
	return &ExclusiveLock{file: file}, nil
}

// Unlock releases the exclusive lock.
func (l *ExclusiveLock) Unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	l.file = nil
	if err != nil {
		return wrap("unlock", "", err)
	}
	return nil
}

func tightenPath(path string, dir bool) error {
	mode := uint32(privateFileMode)
	if dir {
		mode = uint32(privateDirMode)
	}
	if err := unix.Chmod(path, mode); err != nil {
		return wrap("chmod", path, err)
	}
	return nil
}

// TightenPrivateFile tightens the file permissions to 0600.
func TightenPrivateFile(path string) error {
	return tightenPath(path, false)
}

// TightenPrivateDirectory tightens the directory permissions to 0700.
func TightenPrivateDirectory(path string) error {
	return tightenPath(path, true)
}

// OpenLockFile creates a stable private inode or opens the existing one without
// replacing it. Opening and creation use the same pinned parent descriptor.
func OpenLockFile(root, path string) (*os.File, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDWR|unix.O_CREAT, uint32(privateFileMode))
	if err != nil {
		return nil, mapOpenErr("lock-open", path, err)
	}
	if err = requireRegular(fd, path); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
