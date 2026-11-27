package server

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/statemachine/serverclaim"
	"github.com/dualface/kander/v2/internal/statemachine/servergrouplease"
)

func errOwnedRecoveryNotStopped() error {
	return apiError(api.CodeEvidenceIncomplete, "owned recovery needs a confirmed stopped execution")
}

// ownedRecoveryReady is the readiness of an owned recovery, the group kernel's
// recoverOwned event on the facts of the member card: its holder and its
// observation ticket when it is claimed, and no unfinished start. The facts
// are read in the order the server has always checked them, so a refused
// recovery answers the error of the first one that fails.
func (p *project) ownedRecoveryReady(tx *board.Transaction, input api.GroupRecover, h holder) error {
	snapshot, err := tx.Snapshot(input.Request.Member)
	if err != nil {
		return err
	}
	c, err := p.groupLease(input.Request.GroupID, h)
	if err != nil {
		return err
	}
	facts := servergrouplease.RecoverOwned{Stopped: input.Observation.Status == "stopped", Claimed: claimedState(snapshot.Entry.State), Member: true, Ticket: true}
	var failure error
	if c.accepts(requireGroupEvent) {
		if facts.Claimed {
			claim, err := p.requireHolder(snapshot.Entry.TaskID, h)
			if err != nil {
				failure, facts.Member = err, false
			} else if !p.validObservationTicket(snapshot.Entry.TaskID, input.ObservedRevision, claim.Generation, input.ObservationTicket) {
				failure, facts.Ticket = apiError(api.CodeEvidenceIncomplete, "owned recovery needs a fresh stopped observation ticket"), false
			}
		}
		if failure == nil {
			view, err := tx.CurrentExecutionInTransaction(input.Request.Member)
			if err != nil {
				return err
			}
			if facts.Pending = view.Exists && view.Record.Status == board.ExecutionPending; facts.Pending {
				failure = apiError(api.CodeEvidenceIncomplete, "a pending start must be reconciled before coordinator recovery")
			}
		}
	}
	if c.step(func(hdr bool, g, tg uint64) servergrouplease.Event {
		facts.Hdr, facts.G, facts.Tg = hdr, g, tg
		return facts
	}) {
		return nil
	}
	switch {
	case failure != nil:
		return failure
	case !facts.Stopped:
		return errOwnedRecoveryNotStopped()
	}
	return c.holderError()
}

// auditMemberClaim asks the claim kernel to give the member's claim record an
// audit generation: the card is claimed, the request observed the record's
// generation and names a fresh execution identity, no valid lease exists, and
// the record is not pending. A refused request answers the first failing check
// in the order the server has always made them.
func (p *project) auditMemberClaim(current claimRecord, found bool, lost *api.RecoveryLease) error {
	// A status the server never writes cannot be audited: the kernel refuses
	// it like a pending record.
	projected := current
	if _, known := claimStatuses[current.Status]; !known {
		projected.Status = claimPending
	}
	k, err := p.projectClaim(projected, found, true)
	if err != nil {
		return err
	}
	if k.step(k.auditEvent(current, *lost)) {
		return nil
	}
	switch {
	case current.Generation != lost.ExpectedGeneration:
		return apiError(api.CodeLeaseInvalid, "lost recovery generation changed")
	case serverclaim.Valid(k.state, k.params):
		return apiError(api.CodeLeaseActive, "a live member requires its private holder proof and a fresh stopped observation")
	case found && current.Status != api.ClaimActive && current.Status != api.ClaimLost && current.Status != api.ClaimReleased:
		return apiError(api.CodeLeaseInvalid, "member claim must be reconciled before recovery")
	}
	return apiError(api.CodeLeaseInvalid, "lost recovery requires a fresh generation and execution identity")
}

// recoverLostMember is an explicit user-directed reset, not an ordinary group
// write. It allocates a released audit generation, never a runnable task claim.
func (p *project) recoverLostMember(ctx context.Context, key string, input api.GroupRecover, h holder) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := "group-recover:" + input.Request.GroupID
	return p.idempotent(key, operation, input, func() outcome {
		c, err := p.groupLease(input.Request.GroupID, h)
		if err != nil {
			return outcome{err: err}
		}
		request := servergrouplease.RecoverLost{
			Decision: strings.TrimSpace(input.Request.Decision) != "", Unknown: input.Observation.Status == "unknown",
			ExecOk: input.Lost != nil && api.ValidIdentity(input.Lost.ExecutionID),
		}
		if !c.step(func(hdr bool, g, tg uint64) servergrouplease.Event {
			request.Hdr, request.G, request.Tg = hdr, g, tg
			return request
		}) {
			if !request.Decision || !request.Unknown || !request.ExecOk {
				return outcome{err: apiError(api.CodeInvalidRequest, "lost recovery needs a fresh audit execution, explicit decision and an honest unknown predecessor observation")}
			}
			return outcome{err: c.holderError()}
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
			if current.Generation != previous.Generation || current.ExecutionID != previous.ExecutionID {
				return apiError(api.CodeLeaseInvalid, "lost recovery generation changed")
			}
			if err := p.auditMemberClaim(current, currentFound, input.Lost); err != nil {
				return err
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
		next := claimRecord{TaskID: snapshot.Entry.TaskID, ClientID: h.clientID, ExecutionID: input.Lost.ExecutionID, Owner: cmp.Or(previous.Owner, board.MetadataFrom(snapshot.Text, board.FieldOwner), "recovery"), Generation: previous.Generation + 1, Status: api.ClaimReleased, RenewedAt: now, ExpiresAt: now, Handoffs: slices.Clone(previous.Handoffs), BranchSnapshot: previous.BranchSnapshot}
		next.Handoffs = append(next.Handoffs, api.Handoff{At: now, FromGeneration: previous.Generation, ToGeneration: next.Generation, FromClient: previous.ClientID, ToClient: h.clientID, Owner: next.Owner, Decision: input.Request.Decision})
		next.TokenHash = tokenHash(p.server.token(p.info.ID, next.TaskID, next.ExecutionID, next.Generation))
		extension := board.TransactionExtension{Groups: []string{serviceGroup}, Published: p.indexPublication, Before: validate, After: func(tx *board.Transaction) error {
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
