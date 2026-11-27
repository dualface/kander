package server

import (
	"net/http"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

func (s *Server) registerDecisionRoutes(mux *routeTable, task string) {
	s.projectRoute(mux, "GET "+task+"/decisions", func(p *project, r *http.Request) outcome {
		id, err := canonicalTaskID(r.PathValue("task"))
		if err != nil {
			return outcome{err: err}
		}
		records, err := board.Decisions(p.board, id)
		return result(http.StatusOK, api.DecisionList{Decisions: records}, err)
	})
	// The two route patterns let the host authorize human answers separately.
	for _, kind := range []api.DecisionKind{api.DecisionHuman, api.DecisionLeader} {
		s.projectRoute(mux, "POST "+task+"/decisions/"+string(kind), func(p *project, r *http.Request) outcome {
			var input api.DecisionInput
			if err := decode(r, &input); err != nil {
				return outcome{err: err}
			}
			actor, err := attribution(r, input.RecordedBy)
			if err != nil {
				return outcome{err: err}
			}
			if _, hooked := r.Context().Value(attributionKey{}).(callerAttribution); hooked && strings.TrimSpace(actor) == "" {
				return outcome{err: apiError(api.CodeAttributionRequired, config.Text("remote.error_attribution_required"))}
			}
			input.RecordedBy = actor
			id, scope := r.PathValue("task"), scopeOf(r)
			return p.writeDecision(id, scope, "decision-"+string(kind), input, http.StatusCreated, func(tx *board.Transaction) (any, error) {
				return tx.RecordDecision(id, kind, input, s.now())
			})
		})
	}
	s.projectRoute(mux, "POST "+task+"/decision-mode", func(p *project, r *http.Request) outcome {
		var input api.SetDecisionMode
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		id, scope := r.PathValue("task"), scopeOf(r)
		return p.writeDecision(id, scope, "decision-mode", input, http.StatusOK, func(tx *board.Transaction) (any, error) {
			if err := tx.SetDecisionMode(id, input.Mode); err != nil {
				return nil, err
			}
			staged, err := tx.StagedSnapshot(id)
			return api.DecisionModeResult{TaskID: id, Mode: input.Mode, Revision: staged.Revision}, err
		})
	})
}

func canonicalTaskID(id string) (string, error) {
	canonical, err := board.NormalizeTaskID(id)
	if err != nil {
		return "", err
	}
	// Authorize and apply the same resource name, without file aliases.
	if canonical != id {
		return "", apiError(api.CodeInvalidRequest, config.Text("board.invalid_task_id", id))
	}
	return canonical, nil
}

func (p *project) writeDecision(id string, scope receiptScope, action string, input any, status int, apply func(*board.Transaction) (any, error)) outcome {
	if _, err := canonicalTaskID(id); err != nil {
		return outcome{err: err}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := action + ":" + id
	run := func() outcome {
		claim, _, err := p.readClaim(id)
		if err != nil {
			return outcome{err: err}
		}
		var body any
		lock := board.LockScope{Tasks: []string{id}, Groups: []string{serviceGroup}}
		err = p.boardTransaction(lock, func(tx *board.Transaction) error {
			var err error
			body, err = apply(tx)
			if err != nil {
				return err
			}
			return p.stageOperationReceiptStatus(tx, scope, operation, input, status, body)
		})
		if err != nil {
			return outcome{err: err}
		}
		p.changed(id, claim.Generation)
		return outcome{status: status, body: body}
	}
	// The completed receipt is inseparable from the board write. A recovered
	// pending receipt has no mutation to infer from a later card revision.
	return p.idempotent(scope, operation, input, run, func() (outcome, bool) { return outcome{}, false })
}
