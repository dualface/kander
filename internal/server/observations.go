package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (s *Server) observationRoutes(mux *http.ServeMux) {
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
		p.mu.Lock()
		defer p.mu.Unlock()
		key, operation := r.Header.Get(api.HeaderIdempotencyKey), "observation:"+id
		return p.idempotent(key, operation, input, func() outcome {
			var view api.ClaimView
			err := board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{id}}, func(tx *board.Transaction) error {
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
				return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Key: key, Operation: operation, Digest: requestDigest(operation, input), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()})
			})
			if err == nil {
				_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: id, Generation: view.Generation})
			}
			return result(http.StatusOK, view, err)
		}, func() (outcome, bool) { return outcome{}, false })
	})
}
