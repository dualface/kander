package fs

import "time"

// renameBackoff lists the waits between the attempts of a rename that another handle keeps busy. The waits add up to
// 630ms, so a persistently busy target fails after well under a second. Tests may replace it with a shorter list.
var renameBackoff = []time.Duration{
	10 * time.Millisecond,
	20 * time.Millisecond,
	40 * time.Millisecond,
	80 * time.Millisecond,
	160 * time.Millisecond,
	320 * time.Millisecond,
}

// nextRenameWait decides what follows the failed attempt with the zero-based index attempt. Only a busy failure is
// retried, and only while the backoff list has a wait left; the wait is the entry for that attempt.
func nextRenameWait(backoff []time.Duration, attempt int, busy bool) (time.Duration, bool) {
	if !busy || attempt >= len(backoff) {
		return 0, false
	}
	return backoff[attempt], true
}
