package server

import (
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// ReviewDispositionSubmission carries the card CAS separately from the author's
// immutable disposition original.
type ReviewDispositionSubmission struct {
	Disposition      board.ReviewDisposition `json:"disposition"`
	ExpectedRevision uint64                  `json:"expected_revision"`
}

func (s *Server) reviewActionRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/reviews"
	s.projectRoute(mux, "GET "+base+"/plans/{plan}", func(p *project, r *http.Request) outcome {
		value, err := board.ReadReviewPlan(p.board, r.PathValue("plan"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET "+base+"/batches/{batch}", func(p *project, r *http.Request) outcome {
		value, err := board.ReadReviewBatch(p.board, r.PathValue("batch"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET "+base+"/runs/{run}", func(p *project, r *http.Request) outcome {
		value, found, err := board.LookupReviewRun(p.board, r.PathValue("run"))
		if err == nil && !found {
			err = apiError(api.CodeNotFound, "review run does not exist")
		}
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base+"/actions/{action}", func(p *project, r *http.Request) outcome {
		key, action, h := r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("action"), holderOf(r)
		switch action {
		case "resolution-facts":
			var request board.ReviewPlanExtension
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.resolutionFacts(request)
		case "resolve-batch":
			var request api.ReviewResolution
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.resolveReview(key, request, h)
		case "extend-plan":
			var request board.ReviewPlanExtension
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.reviewWrite(key, "review-extend-plan", request, func() ([]string, error) {
				plan, err := board.ReadReviewPlan(p.board, request.PlanID)
				return plan.TaskIDs, err
			}, h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"plan_id": request.PlanID}, board.ExtendReviewPlanWithTransaction(p.board, request, extension)
			})
		case "advance":
			var request board.ReviewBatchAdvance
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.reviewWrite(key, "review-advance", request, func() ([]string, error) {
				batch, err := board.ReadReviewBatch(p.board, request.BatchID)
				if err != nil {
					return nil, err
				}
				if batch.PlanID == "" {
					return batch.TaskIDs, nil
				}
				plan, err := board.ReadReviewPlan(p.board, batch.PlanID)
				return plan.TaskIDs, err
			}, h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"batch_id": request.BatchID}, board.AdvanceReviewBatchWithTransaction(p.board, request, extension)
			})
		case "assign":
			var request board.ReviewAssignment
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.reviewWrite(key, "review-assign", request, p.runTasks(request.RunID), h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"run_id": request.RunID}, board.AssignReviewFindingsWithTransaction(p.board, request, extension)
			})
		case "disposition":
			var request ReviewDispositionSubmission
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			// An author owns only the assigned card, not every card in a joint run.
			return p.reviewWrite(key, "review-disposition", request, func() ([]string, error) { return []string{request.Disposition.TaskID}, nil }, h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"record_id": request.Disposition.RecordID}, board.SubmitReviewDispositionWithTransaction(p.board, request.Disposition, request.ExpectedRevision, extension)
			})
		case "interpret":
			var request board.ReviewInterpretation
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.reviewWrite(key, "review-interpret", request, p.runTasks(request.RunID), h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"run_id": request.RunID}, board.InterpretReviewWithTransaction(p.board, request, extension)
			})
		case "map-legacy":
			var request board.LegacyFindingMap
			if err := decode(r, &request); err != nil {
				return outcome{err: err}
			}
			return p.reviewWrite(key, "review-map-legacy", request, p.runTasks(request.RunID), h, func(extension board.TransactionExtension) (any, error) {
				return map[string]string{"run_id": request.RunID}, board.MapLegacyReviewWithTransaction(p.board, request, extension)
			})
		default:
			return outcome{err: apiError(api.CodeInvalidRequest, "unsupported review action")}
		}
	})
}

func (p *project) runTasks(run string) func() ([]string, error) {
	return func() ([]string, error) { value, err := board.ReadReviewRun(p.board, run); return value.TaskIDs, err }
}
