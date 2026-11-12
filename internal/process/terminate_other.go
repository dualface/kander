//go:build !linux && !windows

package process

import "context"

// TerminateObserved refuses an unpinned PID on platforms without a process
// handle that can both verify the recorded incarnation and receive a signal.
func TerminateObserved(context.Context, ProcessFacts, string) error { return ErrProcessUnsupported }
