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
	return p.confirm(r.Header.Get(api.HeaderIdempotencyKey), id, req, actor)
}

func (p *project) confirm(key, id string, req api.Confirm, actor string) outcome {
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
	ran := false
	run := func() outcome {
		ran = true
		var task api.Task
		extension := p.taskTransaction(key, operation, request, id, nil, nil, false, func(_ *board.Transaction, staged *api.Task) error {
			task = *staged
			return nil
		})
		input := board.ConfirmInput{ExpectedRevision: req.ExpectedRevision, ConfirmedBy: actor, At: p.server.now()}
		if err := board.ConfirmRequest(p.board, id, input, extension); err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: task}
	}
	// A successful board transaction already contains the exact completed receipt.
	out := p.idempotent(key, operation, request, run, func() (outcome, bool) { return outcome{}, false })
	if ran && out.err == nil && out.status == http.StatusOK {
		p.changed(id, 0)
	}
	return out
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
