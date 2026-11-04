package server

import (
	"context"
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// startCursor is the board cursor a launcher start holds between its claim
// and its confirmation. The board fences execution writes by the cursor of
// the start; the server keeps it in memory for the holder and rebuilds a
// bound cursor from the current execution record after a restart.
type startCursor struct {
	entry      board.Entry
	generation uint64
}

// cursor returns the execution cursor of task for generation.
func (p *project) cursor(task string, generation uint64) (board.Entry, error) {
	if c, ok := p.starts[task]; ok && c.generation == generation {
		return c.entry, nil
	}
	s, err := board.ReadSnapshot(p.board, task)
	if err != nil {
		return board.Entry{}, err
	}
	if _, err := board.BindExecutionCursor(p.board, s.Entry); err != nil {
		return board.Entry{}, err
	}
	return s.Entry, nil
}

// execution applies one launcher fact of the live holder.
func (p *project) execution(key, id string, u api.ExecutionUpdate, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		id, err := board.NormalizeTaskID(id)
		if err != nil {
			return outcome{err: err}
		}
		claim, err := p.requireHolder(id, h)
		if err != nil {
			return outcome{err: err}
		}
		entry, err := p.cursor(id, claim.Generation)
		if err != nil {
			return outcome{err: err}
		}
		switch u.Action {
		case api.ExecutionWindow:
			_, err = board.RecordExecutionWindow(p.board, entry, u.Window, u.ExecResolved)
		case api.ExecutionSession:
			_, err = board.RecordExecutionSession(context.Background(), p.board, entry, u.SessionBefore, u.SessionAfter)
		case api.ExecutionConfirm:
			if u.SessionBefore != "" || u.SessionAfter != "" {
				err = board.ConfirmTaskStartSession(p.board, entry, u.SessionBefore, u.SessionAfter)
			} else {
				err = board.ConfirmTaskStart(p.board, entry)
			}
			if err == nil {
				// A confirmed start is no longer a failed launch to undo.
				delete(p.starts, id)
				if claim.OriginalText != "" {
					claim.OriginalText = ""
					err = p.writeClaim(claim)
				}
			}
		case api.ExecutionRollback:
			if claim.OriginalText == "" {
				return outcome{err: apiError(api.CodeInvalidRequest, "only a launcher start can be rolled back")}
			}
			if err = board.RollbackDocument(p.board, entry, claim.OriginalText, "todo"); err == nil {
				delete(p.starts, id)
				claim.Status = api.ClaimReleased
				if werr := p.writeClaim(claim); werr != nil {
					p.log().Error("claim release failed", "task", id, "error", werr)
				}
				_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: id, Generation: claim.Generation})
			}
		default:
			return outcome{err: apiError(api.CodeInvalidRequest, "unknown execution action")}
		}
		if err != nil {
			return outcome{err: err}
		}
		p.changed(id, claim.Generation)
		t, err := p.show(id)
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: t}
	}
	// Execution writes are fenced by the board ledger; a retry of an applied
	// confirmation is a board replay, and a retried window or session write
	// of the same value changes nothing.
	return p.idempotent(key, "execution:"+id, u, run, func() (outcome, bool) { return outcome{}, false })
}
