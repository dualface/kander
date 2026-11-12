package server

import (
	"net/http"
	"reflect"
	"slices"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *project) resolutionTasks(input board.ReviewPlanExtension) ([]string, error) {
	plan, err := board.ReadReviewPlan(p.board, input.PlanID)
	if err != nil {
		return nil, err
	}
	if input.ResolveBatch == nil {
		return nil, apiError(api.CodeInvalidRequest, "resolution request required")
	}
	batch, err := board.ReadReviewBatch(p.board, input.ResolveBatch.BatchID)
	if err != nil {
		return nil, err
	}
	return slices.Compact(slices.Sorted(slices.Values(slices.Concat(plan.TaskIDs, batch.TaskIDs)))), nil
}

func (p *project) resolutionFacts(input board.ReviewPlanExtension) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	facts, err := board.ReviewBatchResolutionFacts(p.board, input)
	return result(http.StatusOK, facts, err)
}

func (p *project) resolveReview(key string, input api.ReviewResolution, h holder) outcome {
	return p.reviewWrite(key, "review-resolve-batch", input, func() ([]string, error) { return p.resolutionTasks(input.Extension) }, h, func(extension board.TransactionExtension) (any, error) {
		ids, err := p.resolutionTasks(input.Extension)
		if err != nil {
			return nil, err
		}
		guard := board.TransactionExtension{Before: func(tx *board.Transaction) error {
			facts, err := board.ReviewBatchResolutionFactsInTransaction(tx, input.Extension)
			if err != nil {
				return err
			}
			facts.TaskIDs = ids
			if !reflect.DeepEqual(facts, input.Facts) {
				return apiError(api.CodeEvidenceIncomplete, "historical resolution originals changed since inspection")
			}
			return nil
		}}
		return map[string]string{"plan_id": input.Extension.PlanID}, board.ResolveReviewBatchWithTransaction(p.board, input.Extension, input.Git, extension, guard)
	})
}
