package server

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// recoverLostMember is an explicit user-directed reset, not an ordinary group
// write. It allocates a released audit generation, never a runnable task claim.
func (p *project) recoverLostMember(ctx context.Context, key string, input api.GroupRecover, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "group-recover:" + input.Request.GroupID
	return p.idempotent(key, operation, input, func() outcome {
		if input.Lost == nil || !api.ValidIdentity(input.Lost.ExecutionID) || strings.TrimSpace(input.Request.Decision) == "" || input.Observation.Status != "unknown" {
			return outcome{err: apiError(api.CodeInvalidRequest, "lost recovery needs a fresh audit execution, explicit decision and an honest unknown predecessor observation")}
		}
		if _, err := p.requireGroup(input.Request.GroupID, h); err != nil {
			return outcome{err: err}
		}
		snapshot, err := board.ReadSnapshot(p.board, input.Request.Member)
		if err != nil {
			return outcome{err: err}
		}
		if !claimedState(snapshot.Entry.State) || snapshot.Revision != input.ObservedRevision {
			return outcome{err: apiError(api.CodeRevisionConflict, "lost recovery member state or version changed")}
		}
		previous, previousFound, err := p.readClaim(snapshot.Entry.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		validate := func(tx *board.Transaction) error {
			if _, err := p.requireGroup(input.Request.GroupID, h); err != nil {
				return err
			}
			current, currentFound, err := p.readClaim(snapshot.Entry.TaskID)
			if err != nil {
				return err
			}
			if currentFound != previousFound || currentFound && current.TaskID != snapshot.Entry.TaskID {
				return apiError(api.CodeLeaseInvalid, "lost member claim identity changed")
			}
			if current.Generation != input.Lost.ExpectedGeneration || current.Generation != previous.Generation || current.ExecutionID != previous.ExecutionID {
				return apiError(api.CodeLeaseInvalid, "lost recovery generation changed")
			}
			if current.Status == api.ClaimActive && p.server.now().Before(current.ExpiresAt) {
				return apiError(api.CodeLeaseActive, "a live member requires its private holder proof and a fresh stopped observation")
			}
			if current.Status != api.ClaimActive && current.Status != api.ClaimLost && current.Status != api.ClaimReleased && currentFound {
				return apiError(api.CodeLeaseInvalid, "member claim must be reconciled before recovery")
			}
			if current.Generation == ^uint64(0) || current.ExecutionID == input.Lost.ExecutionID {
				return apiError(api.CodeLeaseInvalid, "lost recovery requires a fresh generation and execution identity")
			}
			if _, err := tx.Expect(snapshot.Entry.TaskID, snapshot.Entry.State, input.ObservedRevision); err != nil {
				return err
			}
			view, err := tx.CurrentExecutionInTransaction(snapshot.Entry.TaskID)
			if err != nil {
				return err
			}
			if view.Exists && view.Record.Status == board.ExecutionPending {
				return apiError(api.CodeEvidenceIncomplete, "a pending start must be reconciled before recovery")
			}
			return nil
		}
		now := p.server.now()
		next := claimRecord{TaskID: snapshot.Entry.TaskID, ClientID: h.clientID, ExecutionID: input.Lost.ExecutionID, Owner: cmp.Or(previous.Owner, board.MetadataFrom(snapshot.Text, board.FieldOwner), "recovery"), Generation: previous.Generation + 1, Status: api.ClaimReleased, RenewedAt: now, ExpiresAt: now, Handoffs: slices.Clone(previous.Handoffs)}
		next.Handoffs = append(next.Handoffs, api.Handoff{At: now, FromGeneration: previous.Generation, ToGeneration: next.Generation, FromClient: previous.ClientID, ToClient: h.clientID, Owner: next.Owner, Decision: input.Request.Decision})
		next.TokenHash = tokenHash(p.server.token(p.info.ID, next.TaskID, next.ExecutionID, next.Generation))
		extension := board.TransactionExtension{Groups: []string{serviceGroup}, Before: validate, After: func(tx *board.Transaction) error {
			if err := validate(tx); err != nil {
				return err
			}
			checkpoint, ok := tx.OperationResult().(board.CoordinatorCheckpoint)
			if !ok {
				return apiError(api.CodeInternal, "recovery producer omitted its checkpoint result")
			}
			after, err := tx.StagedSnapshot(next.TaskID)
			if err != nil {
				return err
			}
			if after.Entry.State != "working" || after.Revision <= snapshot.Revision {
				return apiError(api.CodeInternal, "lost recovery did not reset its member")
			}
			next.LaunchRevision = after.Revision
			if err := stageJSON(tx, "claims/"+next.TaskID+".json", next); err != nil {
				return err
			}
			if err := p.stageSupersedeRequests(tx, next.TaskID, next.Generation); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, key, operation, input, checkpoint)
		}}
		checkpoint, err := board.RecoverCoordinatorWithTransaction(ctx, p.board, input.Request, input.ObservedRevision, extension)
		if err == nil {
			_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: next.TaskID, Generation: next.Generation})
			_ = p.events.append(api.Event{Kind: api.EventGroupChanged, GroupID: input.Request.GroupID})
			p.changed(next.TaskID, next.Generation)
		}
		return result(http.StatusOK, checkpoint, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
