//go:build !linux && !darwin && !windows

package process

import (
	"context"
	"time"
)

// ProcessIdentitySupported reports whether this platform can observe a process.
const ProcessIdentitySupported = false

// ImageCanChange reports whether a running process can replace its image.
const ImageCanChange = true

// ObserveProcess has no implementation on this platform.
func ObserveProcess(ctx context.Context, _ int) (ProcessFacts, error) {
	if err := ctx.Err(); err != nil {
		return ProcessFacts{}, err
	}
	return ProcessFacts{}, ErrProcessUnsupported
}

// FileAccessTime has no implementation on this platform.
func FileAccessTime(string) (time.Time, error) { return time.Time{}, ErrProcessUnsupported }

// BootIdentity has no implementation on this platform.
func BootIdentity() (string, error) { return "", ErrProcessUnsupported }
