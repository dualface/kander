//go:build !linux && !darwin && !windows

package process

import "context"

// ProcessIdentitySupported reports whether this platform can observe a process.
const ProcessIdentitySupported = false

// ObserveProcess has no implementation on this platform.
func ObserveProcess(ctx context.Context, _ int) (ProcessFacts, error) {
	if err := ctx.Err(); err != nil {
		return ProcessFacts{}, err
	}
	return ProcessFacts{}, ErrProcessUnsupported
}

// BootIdentity has no implementation on this platform.
func BootIdentity() (string, error) { return "", ErrProcessUnsupported }
