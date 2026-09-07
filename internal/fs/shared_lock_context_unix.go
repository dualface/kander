//go:build unix

package fs

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryContextLock(file *os.File, shared bool) (bool, error) {
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	err := unix.Flock(int(file.Fd()), mode|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return err == nil, err
}
