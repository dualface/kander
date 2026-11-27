package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

func groupClaimName(group string) string { return "group-claims/" + group + ".json" }
func (p *project) readGroupClaim(group string) (claimRecord, bool, error) {
	var claim claimRecord
	found, err := readJSON(p.root, servicePath(groupClaimName(group)), &claim)
	return claim, found, err
}

// requireGroup checks that h is the current holder of the group lease. The
// decision is the group kernel's require event (group_lease_kernel.go).
func (p *project) requireGroup(group string, h holder) (claimRecord, error) {
	c, err := p.groupLease(group, h)
	if err != nil {
		return claimRecord{}, err
	}
	if !c.step(requireGroupEvent) {
		return c.record, c.holderError()
	}
	return c.record, nil
}
func (p *project) groupView(ctx context.Context, group string) (api.GroupView, error) {
	value := api.GroupView{GroupID: group}
	checkpoint, found, err := board.LookupCoordinatorCheckpoint(ctx, p.board, group)
	if err != nil {
		return value, err
	}
	if found {
		value.Checkpoint = &checkpoint
	}
	claim, found, err := p.readGroupClaim(group)
	if err == nil && found {
		value.Claim = p.groupClaimView(claim)
	}
	return value, err
}
func (p *project) claimGroup(ctx context.Context, key, group, client string, input api.GroupClaim, h holder) outcome {
	if !api.ValidIdentity(input.ExecutionID) || !api.ValidIdentity(client) || input.Request.GroupID != group {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid coordinator execution identity")}
	}
	// The policy context is checked, never part of the claim identity: a
	// resend of the same key after a policy refresh is the same group claim.
	revision, branchMode := input.PolicyRevision, input.BranchMode
	input.PolicyRevision, input.BranchMode = 0, ""
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := "group-claim:" + group
	check := func() error { return p.checkSnapshotClaim(p.groupClaimCreatesSnapshot(group), revision, branchMode) }
	out := p.idempotentChecked(key, operation, input, check, func() outcome {
		previous, found, err := p.readGroupClaim(group)
		if err != nil {
			return outcome{err: err}
		}
		// The first group claim of a group fixes its branch model; every later
		// generation carries it on.
		snapshot := previous.BranchSnapshot
		if !found {
			if snapshot, err = p.newBranchSnapshot(api.BranchSnapshotGroup, branchMode); err != nil {
				return outcome{err: err}
			}
		}
		reuse, err := p.groupClaim(previous, found, client, input, h, "the coordinator lease is still active")
		if err != nil {
			return outcome{err: err}
		}
		claim := previous
		if !reuse {
			now := p.server.now()
			claim = claimRecord{TaskID: group, ClientID: client, ExecutionID: input.ExecutionID, Owner: input.Request.Owner, Generation: previous.Generation + 1, Status: api.ClaimActive, RenewedAt: now, ExpiresAt: now.Add(p.server.ttl), Handoffs: previous.Handoffs, BranchSnapshot: snapshot}
			if found {
				claim.Handoffs = append(claim.Handoffs, api.Handoff{At: now, FromGeneration: previous.Generation, ToGeneration: claim.Generation, FromClient: previous.ClientID, ToClient: client, Owner: claim.Owner, Decision: input.Decision})
			}
			claim.TokenHash = tokenHash(p.server.token(p.info.ID, "group:"+group, claim.ExecutionID, claim.Generation))
		}
		request := input.Request
		request.Token = "coordinator-" + tokenHash(claim.ExecutionID+":"+strconv.FormatUint(claim.Generation, 10))
		extension := board.TransactionExtension{Groups: []string{serviceGroup}, Published: p.indexPublication, Before: func(*board.Transaction) error {
			current, exists, err := p.readGroupClaim(group)
			if err != nil {
				return err
			}
			if exists != found || current.Generation != previous.Generation {
				return apiError(api.CodeLeaseInvalid, "coordinator changed before claim transaction")
			}
			if _, err := p.groupClaim(current, exists, client, input, h, "coordinator lease became active before takeover"); err != nil {
				return err
			}
			// A reuse refreshes the lease like a renewal, also after it lapsed.
			now := p.server.now()
			claim.RenewedAt, claim.ExpiresAt = now, now.Add(p.server.ttl)
			if !reuse && found {
				claim.Handoffs[len(claim.Handoffs)-1].At = now
			}

			return nil
		}, After: func(tx *board.Transaction) error {
			checkpoint, ok := tx.OperationResult().(board.CoordinatorCheckpoint)
			if !ok {
				return apiError(api.CodeInternal, "coordinator producer omitted its result")
			}
			if err := stageJSON(tx, groupClaimName(group), claim); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, key, operation, input, api.GroupGrant{GroupID: group, Checkpoint: checkpoint, Claim: *p.groupClaimView(claim), RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)})
		}}
		checkpoint, err := board.ClaimCoordinatorWithTransaction(ctx, p.board, request, extension)
		if err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventGroupChanged, GroupID: group, Generation: claim.Generation})
		return outcome{status: http.StatusOK, body: api.GroupGrant{GroupID: group, Checkpoint: checkpoint, Claim: *p.groupClaimView(claim), RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)}}
	}, func() (outcome, bool) { return outcome{}, false })
	if out.err == nil && out.status == http.StatusOK {
		raw, err := json.Marshal(out.body)
		if err != nil {
			return outcome{err: err}
		}
		var grant api.GroupGrant
		if err := json.Unmarshal(raw, &grant); err != nil {
			return outcome{err: err}
		}
		grant.Token = p.server.token(p.info.ID, "group:"+group, grant.Claim.ExecutionID, grant.Claim.Generation)
		out.body = grant
	}
	return out
}
func (p *project) renewGroup(group string, h holder) (api.Renewal, error) {
	if err := p.lockBoard(); err != nil {
		return api.Renewal{}, err
	}
	defer p.unlockBoard()
	var claim claimRecord
	err := p.boardTransaction(board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		c, err := p.groupLease(group, h)
		if err != nil {
			return err
		}
		if !c.step(renewGroupEvent) {
			return c.holderError()
		}
		claim = c.record
		claim.RenewedAt = p.server.now()
		claim.ExpiresAt = claim.RenewedAt.Add(p.server.ttl)
		return stageJSON(tx, groupClaimName(group), claim)
	})
	if err != nil {
		return api.Renewal{}, err
	}
	return p.policyRenewal(api.Renewal{Claim: *p.groupClaimView(claim)}), nil
}
