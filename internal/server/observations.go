package server

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

func (s *Server) observationRoutes(mux *routeTable) {
	s.projectRoute(mux, "POST /api/v1/projects/{project}/tasks/{task}/observation", func(p *project, r *http.Request) outcome {
		var input api.AgentObservation
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		if !slices.Contains([]string{"alive", "stopped", "unknown"}, input.Status) || len(input.Reason) > 4096 || len(input.RuntimeState) > 32 {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid agent observation")}
		}
		id, err := board.NormalizeTaskID(r.PathValue("task"))
		if err != nil {
			return outcome{err: err}
		}
		if err := p.lockBoard(); err != nil {
			return outcome{err: err}
		}
		defer p.unlockBoard()
		scope, operation := scopeOf(r), "observation:"+id
		// An observation keeps one receipt slot per execution, not one
		// receipt per key: the agents of every execution send one each round.
		presented, _ := holderOf(r).forTask(id)
		slot := liveReceipt(operation, presented.token)
		return p.idempotentAt(slot, scope, operation, input, nil, func() outcome {
			var view api.ClaimView
			err := p.boardTransaction(board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{id}}, func(tx *board.Transaction) error {
				claim, err := p.requireHolder(id, holderOf(r))
				if err != nil {
					return err
				}
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if !claimedState(snapshot.Entry.State) {
					return apiError(api.CodeLeaseInvalid, "execution has already left its claimed state")
				}
				observation := input
				observation.ObservedAt = p.server.now()
				claim.Observation = &observation
				if err := stageJSON(tx, "claims/"+id+".json", claim); err != nil {
					return err
				}
				view = *claim.view(p.server.now())
				data, err := json.Marshal(view)
				if err != nil {
					return err
				}
				return stageJSON(tx, slot.name, receipt{Key: scope.key, Operation: operation, Digest: requestDigest(operation, input), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()})
			})
			if err == nil {
				_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: id, Generation: view.Generation})
			}
			return result(http.StatusOK, view, err)
		}, func() (outcome, bool) { return outcome{}, false })
	})
}
