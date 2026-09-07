//go:build !windows

package liveness

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Nonblocking writes avoid a goroutine stuck inside a full stdout pipe. The
// caller grants exclusive use; descriptor flags are restored after writer join.
func subscriptionFileWriter(file *os.File) (ContextWriter, func() error, error) {
	fd := int(file.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return nil, nil, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}
	cleanup := func() error { _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags); return err }
	writer := contextWriteFunc(func(ctx context.Context, p []byte) (int, error) {
		defer runtime.KeepAlive(file)
		total := 0
		for len(p) > 0 {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			n, err := unix.Write(fd, p)
			if n > 0 {
				total += n
				p = p[n:]
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				timer := time.NewTimer(5 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return total, ctx.Err()
				case <-timer.C:
				}
				continue
			}
			if err != nil {
				return total, err
			}
			if n == 0 {
				return total, syscall.EIO
			}
		}
		return total, nil
	})
	return writer, cleanup, nil
}
