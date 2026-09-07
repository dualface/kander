//go:build unix

package fs

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func trySharedLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_SH|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return err == nil, err
}
