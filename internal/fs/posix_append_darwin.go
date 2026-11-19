//go:build darwin

package fs

import "time"

// appendCreateBackoff lists the waits between the append opens of one OpenAppendFile call. On macOS an open with
// O_CREAT but without O_EXCL of a file that another goroutine or process is creating at the same moment has been
// seen to fail with ENOENT although the pinned parent directory exists; the kernel cause is not confirmed. The
// waits add up to 63ms, so a parent that was really removed still fails quickly. Tests may replace it.
var appendCreateBackoff = []time.Duration{
	1 * time.Millisecond,
	2 * time.Millisecond,
	4 * time.Millisecond,
	8 * time.Millisecond,
	16 * time.Millisecond,
	32 * time.Millisecond,
}
