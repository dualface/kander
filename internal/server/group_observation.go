package server

import (
	"net/http"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *project) updateGroupLease(key, group, operation string, input any, h holder, update func(*claimRecord) error) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.idempotent(key, operation, input, func() outcome {
		var view api.ClaimView
		err := board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
			claim, err := p.requireGroup(group, h)
			if err != nil {
				return err
			}
			if err := update(&claim); err != nil {
				return err
			}
			if err := stageJSON(tx, groupClaimName(group), claim); err != nil {
				return err
			}
			view = *claim.view(p.server.now())
			return p.stageOperationReceipt(tx, key, operation, input, api.Renewal{Claim: view})
		})
		if err == nil {
			_ = p.events.append(api.Event{Kind: api.EventGroupChanged, GroupID: group, Generation: view.Generation})
		}
		return result(http.StatusOK, api.Renewal{Claim: view}, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
func (p *project) observeGroup(r *http.Request) outcome {
	var observation api.AgentObservation
	if err := decode(r, &observation); err != nil {
		return outcome{err: err}
	}
	if observation.Status != "alive" && observation.Status != "stopped" && observation.Status != "unknown" || len(observation.Reason) > 4096 || len(observation.RuntimeState) > 32 {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid coordinator observation")}
	}
	group := r.PathValue("group")
	return p.updateGroupLease(r.Header.Get(api.HeaderIdempotencyKey), group, "group-observation:"+group, observation, holderOf(r), func(claim *claimRecord) error {
		observation.ObservedAt = p.server.now()
		claim.Observation = &observation
		if observation.Status == "stopped" {
			claim.Status = api.ClaimLost
			claim.ExpiresAt = p.server.now()
		}
		return nil
	})
}
func (p *project) releaseGroup(r *http.Request) outcome {
	var input api.GroupRelease
	if err := decode(r, &input); err != nil {
		return outcome{err: err}
	}
	group := r.PathValue("group")
	if input.GroupID != group || strings.TrimSpace(input.Decision) == "" || strings.TrimSpace(input.Reason) == "" {
		return outcome{err: apiError(api.CodeInvalidRequest, "group release needs its identity, reason and decision")}
	}
	return p.updateGroupLease(r.Header.Get(api.HeaderIdempotencyKey), group, "group-release:"+group, input, holderOf(r), func(claim *claimRecord) error { claim.Status = api.ClaimReleased; return nil })
}
