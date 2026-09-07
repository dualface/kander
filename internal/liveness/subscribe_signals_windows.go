package liveness

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func subscriptionSignalContext() (context.Context, context.CancelFunc) {
	// Go maps console close, logoff and shutdown notifications to SIGTERM.
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
