//go:build !windows

package tui

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitReadableWorks reports that terminal replies arrive as a byte stream and
// the input descriptor can be polled, so the runtime background probe is able
// to run on this platform.
const waitReadableWorks = true

// waitReadable bounds the wait for the continuation of a possible response
// fragment: it polls the input descriptor without consuming bytes, so the
// probe never competes with Bubble Tea's input loop and never blocks longer
// than d.
func waitReadable(fd uintptr, d time.Duration) bool {
	if d <= 0 {
		return false
	}
	var set unix.FdSet
	set.Set(int(fd)) //nolint:gosec
	tv := unix.NsecToTimeval(d.Nanoseconds())
	for {
		n, err := unix.Select(int(fd)+1, &set, nil, nil, &tv) //nolint:gosec
		if err == unix.EINTR {
			continue
		}
		return err == nil && n > 0
	}
}
