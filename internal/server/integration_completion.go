package server

import (
	"net/http"
	"strconv"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func taskIntegrationName(task string, generation uint64) string {
	return servicePath("task-integrations", task, strconv.FormatUint(generation, 10)+".json")
}
func (p *project) taskIntegration(task string) (api.Integration, error) {
	id, err := board.NormalizeTaskID(task)
	if err != nil {
		return api.Integration{}, err
	}
	claim, found, err := p.readClaim(id)
	if err != nil {
		return api.Integration{}, err
	}
	if !found {
		return api.Integration{}, apiError(api.CodeNotFound, "task has no execution integration")
	}
	var value api.Integration
	found, err = readJSON(p.root, taskIntegrationName(id, claim.Generation), &value)
	if err != nil {
		return value, err
	}
	if !found {
		progress, reviewErr := board.IntegrationReviewProgress(p.board, id)
		delivery, deliveryErr := p.latestDelivery(id)
		if reviewErr == nil && deliveryErr == nil && progress.Status == "closed" && progress.PlanTarget == "N/A" && (delivery == nil || delivery.Purpose == "baseline") {
			return api.Integration{Phase: "not-applicable", Request: api.IntegrationRequest{Purpose: "deliver", Commit: "N/A", Members: []api.IntegrationMember{{TaskID: id, Generation: claim.Generation}}}}, nil
		}
		return value, apiError(api.CodeNotFound, "task generation has no published final integration")
	}
	return value, nil
}
func (p *project) completionIntegration(tx *board.Transaction, task string, generation uint64) (api.Integration, error) {
	progress, err := board.IntegrationReview(tx, task)
	if err != nil {
		return api.Integration{}, err
	}
	if progress.Status == "closed" && progress.PlanTarget == "N/A" {
		delivery, err := p.latestDelivery(task)
		if err != nil {
			return api.Integration{}, err
		}
		if delivery == nil || delivery.Purpose == "baseline" {
			return api.Integration{Phase: "not-applicable"}, nil
		}
	}
	var value api.Integration
	found, err := readJSON(p.root, taskIntegrationName(task, generation), &value)
	if err == nil && !found {
		err = apiError(api.CodeNotFound, "task generation has no published final integration")
	}
	if err != nil {
		return value, err
	}
	if value.Phase != "integrated" || value.Status != api.ClaimReleased || value.Request.Purpose != "deliver" || !api.CommitSHA(value.ObservedTarget) {
		return value, apiError(api.CodeEvidenceIncomplete, "task needs a published final integration receipt")
	}
	memberFound := false
	for _, member := range value.Request.Members {
		if member.TaskID == task && member.Generation == generation && member.DeliveryCommit != "" {
			memberFound = true
		}
	}
	if !memberFound {
		return value, apiError(api.CodeLeaseInvalid, "integration belongs to another task execution")
	}
	progress, err = board.IntegrationReview(tx, task)
	if err != nil {
		return value, err
	}
	if progress.Status != "closed" || progress.PlanTarget != value.Request.Commit {
		return value, apiError(api.CodeEvidenceIncomplete, "integration no longer matches the closed review target")
	}
	return value, nil
}
func (p *project) wrapUpIntegration(tx *board.Transaction, task string, generation uint64, g board.DispatchIntegration) error {
	if p.info.Remote == "" && g.IntegrationID == "" {
		return nil
	}
	if g.IntegrationID == "" || g.IntegrationExecution == "" || g.IntegrationGeneration == 0 {
		return apiError(api.CodeEvidenceIncomplete, "distributed wrap-up requires the published integration identity")
	}
	value, err := p.integrationExecution(g.IntegrationID, g.IntegrationExecution)
	if err != nil {
		return err
	}
	if value.ID != api.IntegrationID(g.TargetRef) || value.Phase != "integrated" || value.Status != api.ClaimReleased || value.Generation != g.IntegrationGeneration || value.Request.Purpose != "deliver" || value.Request.Commit != g.SourceCommit || value.ObservedTarget != g.TargetCommit || g.CWD != "project:"+p.info.ID {
		return apiError(api.CodeEvidenceIncomplete, "wrap-up does not match the published final integration")
	}
	matches := false
	for _, member := range value.Request.Members {
		if member.TaskID == task && member.Generation == generation && member.DeliveryCommit != "" {
			matches = true
		}
	}
	if !matches {
		return apiError(api.CodeLeaseInvalid, "wrap-up integration belongs to another execution")
	}
	progress, err := board.IntegrationReview(tx, task)
	if err != nil {
		return err
	}
	if progress.Status != "closed" || progress.PlanTarget != g.SourceCommit || g.ReviewTarget != g.SourceCommit {
		return apiError(api.CodeEvidenceIncomplete, "wrap-up integration no longer matches the final review target")
	}
	return nil
}
func (s *Server) taskIntegrationRoutes(mux *http.ServeMux) {
	s.projectRoute(mux, "GET /api/v1/projects/{project}/tasks/{task}/integration", func(p *project, r *http.Request) outcome {
		value, err := p.taskIntegration(r.PathValue("task"))
		return result(http.StatusOK, value, err)
	})
}
