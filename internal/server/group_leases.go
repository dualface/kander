package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func groupClaimName(group string) string { return "group-claims/" + group + ".json" }
func (p *project) readGroupClaim(group string) (claimRecord, bool, error) {
	var claim claimRecord
	found, err := readJSON(p.root, servicePath(groupClaimName(group)), &claim)
	return claim, found, err
}
func (p *project) requireGroup(group string, h holder) (claimRecord, error) {
	claim, found, err := p.readGroupClaim(group)
	if err != nil {
		return claim, err
	}
	if h.invalid != nil {
		return claim, h.invalid
	}
	if !found || claim.Status != api.ClaimActive || claim.Generation != h.generation || !p.server.now().Before(claim.ExpiresAt) {
		return claim, apiError(api.CodeLeaseInvalid, "coordinator lease is not current")
	}
	if h.token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(claim.TokenHash)) != 1 {
		return claim, apiError(api.CodeHeldByOther, "group is held by another coordinator")
	}
	return claim, nil
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
		value.Claim = claim.view(p.server.now())
	}
	return value, err
}
func (p *project) groupReceipt(tx *board.Transaction, key, operation string, request, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()})
}
func (p *project) claimGroup(ctx context.Context, key, group, client string, input api.GroupClaim, h holder) outcome {
	if !api.ValidIdentity(input.ExecutionID) || !api.ValidIdentity(client) || input.Request.GroupID != group {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid coordinator execution identity")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "group-claim:" + group
	out := p.idempotent(key, operation, input, func() outcome {
		previous, found, err := p.readGroupClaim(group)
		if err != nil {
			return outcome{err: err}
		}
		reuse := found && previous.ClientID == client && previous.ExecutionID == input.ExecutionID && previous.Status == api.ClaimActive && p.server.now().Before(previous.ExpiresAt)
		if found && previous.ExecutionID == input.ExecutionID && !reuse {
			return outcome{err: apiError(api.CodeLeaseInvalid, "a lost coordinator execution cannot reuse its identity")}
		}
		claim := previous
		if reuse {
			if _, err := p.requireGroup(group, h); err != nil {
				return outcome{err: err}
			}
		} else {
			if found && previous.Status == api.ClaimActive && p.server.now().Before(previous.ExpiresAt) {
				return outcome{err: apiError(api.CodeLeaseActive, "the coordinator lease is still active")}
			}
			if previous.Generation != input.ExpectedGeneration || previous.Generation == ^uint64(0) {
				return outcome{err: apiError(api.CodeLeaseInvalid, "the observed coordinator generation is not current")}
			}
			if found && strings.TrimSpace(input.Decision) == "" {
				return outcome{err: apiError(api.CodeInvalidRequest, "coordinator takeover needs an explicit decision reference")}
			}
			now := p.server.now()
			claim = claimRecord{TaskID: group, ClientID: client, ExecutionID: input.ExecutionID, Owner: input.Request.Owner, Generation: previous.Generation + 1, Status: api.ClaimActive, RenewedAt: now, ExpiresAt: now.Add(p.server.ttl), Handoffs: previous.Handoffs}
			if found {
				claim.Handoffs = append(claim.Handoffs, api.Handoff{At: now, FromGeneration: previous.Generation, ToGeneration: claim.Generation, FromClient: previous.ClientID, ToClient: client, Owner: claim.Owner, Decision: input.Decision})
			}
			claim.TokenHash = tokenHash(p.server.token(p.info.ID, "group:"+group, claim.ExecutionID, claim.Generation))
		}
		request := input.Request
		request.Token = "coordinator-" + tokenHash(claim.ExecutionID+":"+strconv.FormatUint(claim.Generation, 10))
		extension := board.TransactionExtension{Groups: []string{serviceGroup}, Before: func(*board.Transaction) error {
			current, exists, err := p.readGroupClaim(group)
			if err != nil {
				return err
			}
			if exists != found || current.Generation != previous.Generation {
				return apiError(api.CodeLeaseInvalid, "coordinator changed before claim transaction")
			}
			if reuse {
				_, err := p.requireGroup(group, h)
				return err
			}
			if exists && current.Status == api.ClaimActive && p.server.now().Before(current.ExpiresAt) {
				return apiError(api.CodeLeaseActive, "coordinator lease became active before takeover")
			}
			now := p.server.now()
			claim.RenewedAt, claim.ExpiresAt = now, now.Add(p.server.ttl)
			if found {
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
			return p.groupReceipt(tx, key, operation, input, api.GroupGrant{GroupID: group, Checkpoint: checkpoint, Claim: *claim.view(p.server.now()), RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)})
		}}
		checkpoint, err := board.ClaimCoordinatorWithTransaction(ctx, p.board, request, extension)
		if err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventGroupChanged, GroupID: group, Generation: claim.Generation})
		return outcome{status: http.StatusOK, body: api.GroupGrant{GroupID: group, Checkpoint: checkpoint, Claim: *claim.view(p.server.now()), RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)}}
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
	p.mu.Lock()
	defer p.mu.Unlock()
	var claim claimRecord
	err := board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		var err error
		claim, err = p.requireGroup(group, h)
		if err != nil {
			return err
		}
		claim.RenewedAt = p.server.now()
		claim.ExpiresAt = claim.RenewedAt.Add(p.server.ttl)
		return stageJSON(tx, groupClaimName(group), claim)
	})
	return api.Renewal{Claim: *claim.view(p.server.now())}, err
}
