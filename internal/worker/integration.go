package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/dualface/kander/internal/remote"
)

// An integration lease belongs to the command process that can push the ref.
// It never delivers task requests or reports that command as the task agent.
func (w *loop) integrationWork(ctx context.Context, log *slog.Logger, c remote.Credential) bool {
	observeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	observation := w.observe(observeCtx, c)
	cancel()
	if observation.Status == "stopped" {
		w.lose(log, c, "integration command stopped: "+observation.Reason)
		return false
	}
	return true
}
