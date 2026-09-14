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

// RecoverAcceptedDispatch rotates only a proven stopped accepted execution.
// The caller owns the delivery lease. Unproven observations leave the receipt
// unchanged and return their diagnostic; notify reconciles the receipt on error.
func RecoverAcceptedDispatch(ctx context.Context, root string, d board.Dispatch) (board.Dispatch, error) {
	if d.State != board.DispatchAccepted {
		return d, nil
	}
	s, err := board.ReadSnapshot(root, d.Input.TaskID)
	if err != nil {
		return d, err
	}
	exit, err := observedDispatchExit(ctx, s, d.Input.ID)
	if err != nil {
		return d, err
	}
	if err = ValidateActionEvidence(ctx, root, d); err != nil {
		return d, err
	}
	if err = ctx.Err(); err != nil {
		return d, err
	}
	next, err := board.ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, d.Revision, exit)
	return next, err
}
