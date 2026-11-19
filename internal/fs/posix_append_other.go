//go:build unix && (!darwin || ios)

package fs

import "time"

// appendCreateBackoff is empty here: with a valid pinned parent directory, an append open that returns ENOENT means
// the parent was removed, so a retry would only delay the same error. Tests may replace it.
var appendCreateBackoff []time.Duration
