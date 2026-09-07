//go:build !windows

package liveness

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func subscriptionSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}
