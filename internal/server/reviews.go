package server

import (
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// The review routes carry the evidence commands of the v1 review gate. The
// client runs every Git check against its own repository and sends the facts;
// the server revalidates structure and bindings with the board package, as
// the v1 board layer does, and never runs Git.

// ReviewClose is POST .../reviews/close: the closure request and the Git
// evidence the client verified.
type ReviewClose struct {
	Request  board.ReviewCloseRequest `json:"request"`
	Evidence board.ReviewGitEvidence  `json:"evidence"`
}

// requireHolders checks that h holds every claimed card among tasks. A plan
// over cards of several holders needs a joint confirmation, which this
// version does not offer; it is refused with held_by_other.
func (p *project) requireHolders(tasks []string, h holder) error {
	for _, id := range tasks {
		s, err := board.ReadSnapshot(p.board, id)
		if err != nil {
			return err
		}
		if claimedState(s.Entry.State) {
			if _, err := p.requireHolder(s.Entry.TaskID, h); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *project) reviewWrite(key, operation string, request any, tasks func() ([]string, error), h holder, apply func() (any, error)) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		ids, err := tasks()
		if err != nil {
			return outcome{err: err}
		}
		if err := p.requireHolders(ids, h); err != nil {
			return outcome{err: err}
		}
		body, err := apply()
		if err != nil {
			return outcome{err: err}
		}
		for _, id := range ids {
			p.changed(id, h.generation)
		}
		return outcome{status: http.StatusOK, body: body}
	}
	// Review publications are content-addressed by the board (plan and batch
	// IDs, view hashes); a replay of an applied one is refused by the board as
	// a duplicate, so the receipt is the only replay path.
	return p.idempotent(key, operation, request, run, func() (outcome, bool) { return outcome{}, false })
}

func (p *project) batchTasks(id string) func() ([]string, error) {
	return func() ([]string, error) {
		b, err := board.ReadReviewBatch(p.board, id)
		return b.TaskIDs, err
	}
}

func (s *Server) reviewRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/reviews"
	s.projectRoute(mux, "POST "+base+"/plan", func(p *project, r *http.Request) outcome {
		var plan board.ReviewPlan
		if err := decode(r, &plan); err != nil {
			return outcome{err: err}
		}
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-plan", plan,
			func() ([]string, error) { return plan.TaskIDs, nil }, holderOf(r),
			func() (any, error) {
				return map[string]string{"plan_id": plan.PlanID}, board.CreateReviewPlan(p.board, plan)
			})
	})
	s.projectRoute(mux, "POST "+base+"/batches/{batch}/aggregate", func(p *project, r *http.Request) outcome {
		id := r.PathValue("batch")
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-aggregate:"+id, id, p.batchTasks(id), holderOf(r),
			func() (any, error) { return board.PublishReviewDisposition(p.board, id) })
	})
	s.projectRoute(mux, "GET "+base+"/batches/{batch}/view", func(p *project, r *http.Request) outcome {
		v, err := board.ReadReviewBatchView(p.board, r.PathValue("batch"))
		return result(http.StatusOK, v, err)
	})
	s.projectRoute(mux, "POST "+base+"/close", func(p *project, r *http.Request) outcome {
		var req ReviewClose
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-close:"+req.Request.BatchID, req, p.batchTasks(req.Request.BatchID), holderOf(r),
			func() (any, error) { return board.CloseReviewBatch(p.board, req.Request, req.Evidence) })
	})
	s.projectRoute(mux, "GET "+base+"/progress/{task}", func(p *project, r *http.Request) outcome {
		progress, err := board.ReviewTaskProgress(p.board, r.PathValue("task"))
		if err != nil {
			// The progress body explains what the gate still needs; keep it.
			e := asAPIError(err)
			return outcome{status: e.Status, body: map[string]any{"code": e.Code, "message": e.Message, "board_code": e.BoardCode, "progress": progress}}
		}
		return outcome{status: http.StatusOK, body: progress}
	})
}
