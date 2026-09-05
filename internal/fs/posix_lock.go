//go:build unix

package fs

import (
	"os"

	"golang.org/x/sys/unix"
)

// LockExclusive 在整个文件上取得阻塞式跨进程独占锁.
func LockExclusive(file *os.File) (*ExclusiveLock, error) {
	if file == nil {
		return nil, wrap("lock", "", os.ErrInvalid)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return nil, wrap("lock", file.Name(), err)
	}
	return &ExclusiveLock{file: file}, nil
}

// Unlock 释放独占锁.
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

// TightenPrivateFile 把文件权限收紧为 0600.
func TightenPrivateFile(path string) error {
	return tightenPath(path, false)
}

// TightenPrivateDirectory 把目录权限收紧为 0700.
func TightenPrivateDirectory(path string) error {
	return tightenPath(path, true)
}
