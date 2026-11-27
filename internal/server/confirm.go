package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

const confirmOperation = "confirm:"

func confirmRequest(p *project, r *http.Request) outcome {
	var req api.Confirm
	if err := decode(r, &req); err != nil {
		return outcome{err: err}
	}
	if req.ExpectedRevision == 0 {
		return outcome{err: apiError(api.CodeInvalidRequest, config.Text("remote.confirm_revision_required"))}
	}
	id, err := canonicalTaskID(r.PathValue("task"))
	if err != nil {
		return outcome{err: err}
	}
	actor, err := attribution(r, req.ConfirmedBy)
	if err != nil {
		return outcome{err: err}
	}
	if strings.TrimSpace(actor) == "" {
		if _, hooked := r.Context().Value(attributionKey{}).(callerAttribution); hooked {
			return outcome{err: apiError(api.CodeAttributionRequired, config.Text("remote.error_attribution_required"))}
		}
		return outcome{err: apiError(api.CodeInvalidRequest, config.Text("board.confirmer_required"))}
	}
	return p.confirm(scopeOf(r), id, req, actor)
}

func (p *project) confirm(scope receiptScope, id string, req api.Confirm, actor string) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := confirmOperation + id
	// A retry must match both what the client sent and who the host attributed it to.
	request := struct {
		Input api.Confirm
		Actor string
	}{req, actor}
	run := func() outcome {
		var task api.Task
		extension := p.taskTransaction(scope, operation, request, id, nil, nil, false, func(_ *board.Transaction, staged *api.Task) error {
			task = *staged
			return nil
		})
		input := board.ConfirmInput{ExpectedRevision: req.ExpectedRevision, ConfirmedBy: actor, At: p.server.now()}
		err := board.ConfirmRequest(p.board, id, input, extension)
		committed := err == nil
		if err != nil && task.OperationID != "" {
			// Partitioning can fail after the commit marker is durable. A
			// snapshot refuses a prepared operation and must name this write.
			snapshot, readErr := board.ReadSnapshot(p.board, id)
			committed = readErr == nil && snapshot.OperationID == task.OperationID
		}
		if committed {
			p.changed(id, 0)
		}
		return result(http.StatusOK, task, err)
	}
	// A successful board transaction already contains the exact completed receipt.
	return p.idempotent(scope, operation, request, run, func() (outcome, bool) { return outcome{}, false })
}

func (p *project) requirePublishedConfirmation(id string, body json.RawMessage) error {
	var task api.Task
	if err := json.Unmarshal(body, &task); err != nil {
		return err
	}
	if task.TaskID != id || task.OperationID == "" {
		return errors.New("confirmation receipt does not identify its board operation")
	}
	return board.RequirePublishedOperation(p.board, task.OperationID)
}
