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
