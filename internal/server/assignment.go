package server

import (
	"net/http"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

func (p *project) assign(key, id string, req api.Assign) outcome {
	taskID, err := board.NormalizeTaskID(id)
	if err != nil {
		return outcome{err: err}
	}
	// Authorization sees the matched path. Do not let a legacy filename alias
	// name a different policy resource than the card this request would write.
	if taskID != id {
		return outcome{err: apiError(api.CodeInvalidRequest, config.Text("board.invalid_task_id", id))}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := "assign:" + id
	run := func() outcome {
		var updated api.Task
		extension := p.taskTransaction(key, operation, req, id, nil, nil, false, func(_ *board.Transaction, task *api.Task) error {
			updated = *task
			return nil
		})
		if err := board.Assign(p.board, id, req.Assignee, req.ExpectedRevision, extension); err != nil {
			return outcome{err: err}
		}
		var generation uint64
		if updated.Claim != nil {
			generation = updated.Claim.Generation
		}
		p.changed(id, generation)
		return outcome{status: http.StatusOK, body: updated}
	}
	// The receipt is staged with the card, so a pending intent has no write
	// to infer from a later revision or assignee.
	return p.idempotent(key, operation, req, run, func() (outcome, bool) { return outcome{}, false })
}
