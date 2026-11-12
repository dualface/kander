package server

import (
	"errors"
	"net/http"
	"os"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *project) dispatchWrite(key, action, task string, input api.DispatchCommand, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "dispatch:" + task + ":" + action
	return p.idempotent(key, operation, input, func() outcome {
		if _, err := p.requireHolder(task, h); err != nil {
			return outcome{err: err}
		}
		guard := board.TransactionExtension{Groups: []string{serviceGroup}, Before: func(tx *board.Transaction) error {
			if _, err := p.requireHolder(task, h); err != nil {
				return err
			}
			if input.CardRevision != 0 {
				if _, err := tx.Expect(task, "", input.CardRevision); err != nil {
					return err
				}
			}
			return nil
		}, After: func(tx *board.Transaction) error {
			if _, err := p.requireHolder(task, h); err != nil {
				return err
			}
			for _, id := range tx.ChangedTaskIDs() {
				if _, err := p.requireHolder(id, h); err != nil {
					return err
				}
			}
			dispatch, ok := tx.OperationResult().(board.Dispatch)
			if !ok {
				return apiError(api.CodeInternal, "dispatch producer omitted its transaction result")
			}
			return p.stageOperationReceipt(tx, key, operation, input, dispatch)
		}}
		var value board.Dispatch
		var err error
		switch action {
		case "prepare":
			if input.Input == nil || input.Input.TaskID != task || input.CardRevision == 0 {
				return outcome{err: apiError(api.CodeInvalidRequest, "dispatch preparation needs its task input and card revision")}
			}
			value, err = board.PrepareDispatchWithTransaction(p.board, *input.Input, guard)
		case "attempt":
			if input.CardRevision == 0 {
				return outcome{err: apiError(api.CodeInvalidRequest, "delivery attempt requires its observed card revision")}
			}
			value, err = board.BeginDispatchAttemptWithTransaction(p.board, task, input.DispatchID, input.Revision, []uint64{input.CardRevision}, guard)
		case "reauthorize":
			var exits []board.WrapUpExitEvidence
			if input.Exit != nil {
				exits = append(exits, *input.Exit)
			}
			value, err = board.ReauthorizeDispatchWithTransaction(p.board, task, input.DispatchID, input.Revision, exits, guard)
		case "end":
			var decisions []string
			if input.Decision != "" {
				decisions = append(decisions, input.Decision)
			}
			err = board.EndDispatchWithTransaction(p.board, task, input.DispatchID, input.Revision, input.State, input.Reason, decisions, guard)
		case "settle":
			err = board.SettleDispatchWithTransaction(p.board, task, input.DispatchID, input.Revision, guard)
		case "authorize-wrap-up":
			if input.Exit == nil {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "wrap-up authorization requires fresh exit evidence")}
			}
			value, err = board.AuthorizeDispatchWrapUpWithTransaction(p.board, task, input.DispatchID, input.Revision, input.Author, input.Reason, *input.Exit, guard)
		default:
			return outcome{err: apiError(api.CodeInvalidRequest, "unsupported dispatch producer")}
		}
		if err != nil {
			return outcome{err: err}
		}
		if action == "end" || action == "settle" {
			value, err = board.ReadDispatch(p.board, task, input.DispatchID)
			if err != nil {
				return outcome{err: err}
			}
		}
		p.changed(task, h.generation)
		return outcome{status: http.StatusOK, body: value}
	}, func() (outcome, bool) { return outcome{}, false })
}
func (s *Server) dispatchRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/tasks/{task}/dispatches"
	s.projectRoute(mux, "GET "+base+"/{dispatch}", func(p *project, r *http.Request) outcome {
		value, err := board.ReadDispatch(p.board, r.PathValue("task"), r.PathValue("dispatch"))
		if errors.Is(err, os.ErrNotExist) {
			err = apiError(api.CodeNotFound, "dispatch does not exist")
		}
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base+"/actions/{action}", func(p *project, r *http.Request) outcome {
		task, err := board.NormalizeTaskID(r.PathValue("task"))
		if err != nil {
			return outcome{err: err}
		}
		var input api.DispatchCommand
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.dispatchWrite(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("action"), task, input, holderOf(r))
	})
	s.projectRoute(mux, "GET "+base+"/review-range", func(p *project, r *http.Request) outcome {
		value, err := board.DispatchReviewRange(p.board, r.PathValue("task"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET "+base+"/{dispatch}/validate", func(p *project, r *http.Request) outcome {
		err := board.ValidateDispatchEvidence(p.board, r.PathValue("task"), r.PathValue("dispatch"))
		return result(http.StatusOK, map[string]bool{"valid": err == nil}, err)
	})
}
