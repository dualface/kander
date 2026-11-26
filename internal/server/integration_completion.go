package server

import (
	"net/http"
	"strconv"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/statemachine/serverintegration"
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
		if reviewErr == nil && deliveryErr == nil {
			// Without a receipt the kernel state is empty, so the event accepts only
			// the case that needs no integration.
			empty, err := p.projectIntegration(api.Integration{}, false, nil)
			if err != nil {
				return value, err
			}
			notApplicable := serverintegration.Completion{Closed: progress.Status == "closed", PlanNA: progress.PlanTarget == "N/A", NoDelivery: delivery == nil || delivery.Purpose == "baseline"}
			if empty.accepts(notApplicable) {
				return api.Integration{Phase: "not-applicable", Request: api.IntegrationRequest{Purpose: "deliver", Commit: "N/A", Members: []api.IntegrationMember{{TaskID: id, Generation: claim.Generation}}}}, nil
			}
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
	closed, planNA, noDelivery := progress.Status == "closed", progress.PlanTarget == "N/A", false
	if closed && planNA {
		delivery, err := p.latestDelivery(task)
		if err != nil {
			return api.Integration{}, err
		}
		noDelivery = delivery == nil || delivery.Purpose == "baseline"
	}
	// Without a receipt the kernel state is empty, so the event accepts only the
	// case that needs no integration.
	empty, err := p.projectIntegration(api.Integration{}, false, nil)
	if err != nil {
		return api.Integration{}, err
	}
	if empty.accepts(serverintegration.Completion{Closed: closed, PlanNA: planNA, NoDelivery: noDelivery}) {
		return api.Integration{Phase: "not-applicable"}, nil
	}
	var value api.Integration
	found, err := readJSON(p.root, taskIntegrationName(task, generation), &value)
	if err == nil && !found {
		err = apiError(api.CodeNotFound, "task generation has no published final integration")
	}
	if err != nil {
		return value, err
	}
	k, err := p.projectIntegration(value, true, nil)
	if err != nil {
		return value, err
	}
	member := false
	for _, m := range value.Request.Members {
		if m.TaskID == task && m.Generation == generation && m.DeliveryCommit != "" {
			member = true
		}
	}
	// The review is read again only when the receipt and the member pass, so a
	// refused receipt answers its own error first.
	receipt := k.state.Phase == serverintegration.PhaseIntegrated && k.state.Status == serverintegration.StatusReleased && k.state.Purpose == serverintegration.PurposeDeliver && k.state.Obs
	closed, planIsCommit := false, false
	if receipt && member {
		if progress, err = board.IntegrationReview(tx, task); err != nil {
			return value, err
		}
		closed, planIsCommit = progress.Status == "closed", progress.PlanTarget == value.Request.Commit
	}
	if !k.accepts(serverintegration.Completion{Closed: closed, Member: member, PlanIsCommit: planIsCommit}) {
		switch {
		case !receipt:
			return value, apiError(api.CodeEvidenceIncomplete, "task needs a published final integration receipt")
		case !member:
			return value, apiError(api.CodeLeaseInvalid, "integration belongs to another task execution")
		}
		return value, apiError(api.CodeEvidenceIncomplete, "integration no longer matches the closed review target")
	}
	return value, nil
}
func (p *project) wrapUpIntegration(tx *board.Transaction, task string, generation uint64, g board.DispatchIntegration) error {
	notRequired := p.info.Remote == "" && g.IntegrationID == ""
	if !notRequired && (g.IntegrationID == "" || g.IntegrationExecution == "" || g.IntegrationGeneration == 0) {
		return apiError(api.CodeEvidenceIncomplete, "distributed wrap-up requires the published integration identity")
	}
	var value api.Integration
	var k *integrationKernel
	var err error
	if notRequired {
		k, err = p.projectIntegration(api.Integration{}, false, nil)
	} else if value, err = p.integrationExecution(g.IntegrationID, g.IntegrationExecution); err == nil {
		k, err = p.projectIntegration(value, true, nil)
	}
	if err != nil {
		return err
	}
	member := false
	for _, m := range value.Request.Members {
		if m.TaskID == task && m.Generation == generation && m.DeliveryCommit != "" {
			member = true
		}
	}
	event := serverintegration.WrapUp{
		NotRequired: notRequired, IdGiven: g.IntegrationID != "" && g.IntegrationExecution != "", IdMatch: value.ID == api.IntegrationID(g.TargetRef), Igen: k.generation(g.IntegrationGeneration, value.Generation),
		SrcMatch: value.Request.Commit == g.SourceCommit, TgtMatch: value.ObservedTarget == g.TargetCommit, CwdMatch: g.CWD == "project:"+p.info.ID, Member: member,
	}
	// The published execution must match before the review is read, and the
	// member after it, so a refused execution answers its own error first.
	matches := k.accepts(serverintegration.WrapUp{NotRequired: event.NotRequired, IdGiven: event.IdGiven, IdMatch: event.IdMatch, Igen: event.Igen, SrcMatch: event.SrcMatch, TgtMatch: event.TgtMatch, CwdMatch: event.CwdMatch, Member: true, Closed: true, PlanIsSrc: true, ReviewIsSrc: true})
	if matches && member && !notRequired {
		progress, err := board.IntegrationReview(tx, task)
		if err != nil {
			return err
		}
		event.Closed, event.PlanIsSrc, event.ReviewIsSrc = progress.Status == "closed", progress.PlanTarget == g.SourceCommit, g.ReviewTarget == g.SourceCommit
	}
	if k.accepts(event) {
		return nil
	}
	switch {
	case !matches:
		return apiError(api.CodeEvidenceIncomplete, "wrap-up does not match the published final integration")
	case !member:
		return apiError(api.CodeLeaseInvalid, "wrap-up integration belongs to another execution")
	}
	return apiError(api.CodeEvidenceIncomplete, "wrap-up integration no longer matches the final review target")
}
func (s *Server) taskIntegrationRoutes(mux *routeTable) {
	s.projectRoute(mux, "GET /api/v1/projects/{project}/tasks/{task}/integration", func(p *project, r *http.Request) outcome {
		value, err := p.taskIntegration(r.PathValue("task"))
		return result(http.StatusOK, value, err)
	})
}
