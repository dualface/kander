package launch

import (
	"context"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
)

// observedDispatchExit shares the on-behalf wrap-up standard: the observation
// must be collected for this request and match the complete execution identity.
func observedDispatchExit(ctx context.Context, s board.Snapshot, dispatchID string) (board.WrapUpExitEvidence, error) {
	start := time.Now()
	observed := DispatchObservation(ctx, s)
	if err := ctx.Err(); err != nil {
		return board.WrapUpExitEvidence{}, err
	}
	if board.MetadataFrom(s.Text, board.FieldSession) == "" || !observed.ValidFor(s.Entry, s.Text) || observed.Status != liveness.Stopped || observed.ObservedAt.Before(start) || observed.ObservedAt.After(time.Now()) || time.Since(observed.ObservedAt) > 30*time.Second {
		return board.WrapUpExitEvidence{}, launchError("launch.dispatch_recovery_unproven", dispatchID, observed.Detail)
	}
	return board.WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision, Session: observed.Identity.Session, Window: observed.Identity.Window, Owner: observed.Identity.Owner, StartedAt: observed.Identity.StartedAt, ObservedAt: observed.ObservedAt}, nil
}
