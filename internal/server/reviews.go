package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"

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

func (p *project) reviewWrite(key, operation string, request any, tasks func() ([]string, error), h holder, apply func(board.TransactionExtension) (any, error)) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		ids, err := tasks()
		if err != nil {
			return outcome{err: err}
		}
		authorityIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			snapshot, err := board.ReadSnapshot(p.board, id)
			if err != nil {
				return outcome{err: err}
			}
			if claimedState(snapshot.Entry.State) {
				authorityIDs = append(authorityIDs, id)
			}
		}
		var publication *api.Publication
		if h.publication != "" {
			value, err := p.publicationBinding(h.publication, operation, request, authorityIDs)
			if err != nil {
				return outcome{err: err}
			}
			if value.Status == "published" {
				return outcome{status: http.StatusOK, body: value.Result}
			}
			publication = &value
		} else {
			if err := p.requireHolders(authorityIDs, h); err != nil {
				return outcome{err: err}
			}
			if len(authorityIDs) > 1 {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "joint review needs each holder's version-bound publication confirmation")}
			}
		}

		extension := board.TransactionExtension{Groups: []string{serviceGroup}, Tasks: ids, Before: func(tx *board.Transaction) error {
			if publication != nil {
				return p.validatePublication(tx, *publication)
			}
			for _, id := range authorityIDs {
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if claimedState(snapshot.Entry.State) {
					if _, err := p.requireHolder(id, h); err != nil {
						return err
					}
				}
			}
			return nil
		}}
		extension.After = func(tx *board.Transaction) error {
			if publication != nil {
				if err := p.validatePublication(tx, *publication); err != nil {
					return err
				}
			}
			for _, id := range tx.ChangedTaskIDs() {
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if publication != nil && claimedState(snapshot.Entry.State) {
					confirmed := false
					for _, member := range publication.Members {
						if member.TaskID == id {
							confirmed = true
							break
						}
					}
					if !confirmed {
						return apiError(api.CodeEvidenceIncomplete, "producer writes a card outside the confirmed member set")
					}
				} else if claimedState(snapshot.Entry.State) {
					if _, err := p.requireHolder(id, h); err != nil {
						return err
					}
				}
			}
			body := tx.OperationResult()
			if body == nil {
				body = reviewStaticResult(request)
			}
			if body == nil {
				return apiError(api.CodeInternal, "review producer omitted its transaction result")
			}
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			if publication != nil {
				publication.Status, publication.PublishedAt, publication.Result = "published", p.server.now(), raw
				if err := stageJSON(tx, publicationName(publication.ID), publication); err != nil {
					return err
				}
			}
			receipt := receipt{Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: http.StatusOK, Body: raw, CreatedAt: p.server.now()}
			return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt)
		}
		body, err := apply(extension)
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
		plan.CWD = "project:" + p.info.ID
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-plan", plan,
			func() ([]string, error) { return plan.TaskIDs, nil }, holderOf(r),
			func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"plan_id": plan.PlanID}, board.CreateReviewPlanWithTransaction(p.board, plan, extension)
			})
	})
	s.projectRoute(mux, "POST "+base+"/batches/{batch}/aggregate", func(p *project, r *http.Request) outcome {
		id := r.PathValue("batch")
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-aggregate:"+id, id, p.batchTasks(id), holderOf(r),
			func(extension board.TransactionExtension) (any, error) {
				return board.PublishReviewDispositionWithTransaction(p.board, id, extension)
			})
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
		req.Evidence.CWD = "project:" + p.info.ID
		return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-close:"+req.Request.BatchID, req, p.batchTasks(req.Request.BatchID), holderOf(r),
			func(extension board.TransactionExtension) (any, error) {
				return board.CloseReviewBatchWithTransaction(p.board, req.Request, req.Evidence, extension)
			})
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

func reviewStaticResult(request any) any {
	switch value := request.(type) {
	case board.ReviewPlan:
		return map[string]string{"plan_id": value.PlanID}
	case board.ReviewPlanExtension:
		return map[string]string{"plan_id": value.PlanID}
	case board.ReviewBatchAdvance:
		return map[string]string{"batch_id": value.BatchID}
	case board.ReviewAssignment:
		return map[string]string{"run_id": value.RunID}
	case ReviewDispositionSubmission:
		return map[string]string{"record_id": value.Disposition.RecordID}
	case board.ReviewInterpretation:
		return map[string]string{"run_id": value.RunID}
	case board.LegacyFindingMap:
		return map[string]string{"run_id": value.RunID}
	}
	return nil
}
