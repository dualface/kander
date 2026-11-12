package server

import (
	"cmp"
	"reflect"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func integrationIDs(input api.IntegrationRequest) ([]string, error) {
	if !api.IntegrationTarget(input.TargetRef) || !api.CommitSHA(input.BaseCommit) || !api.CommitSHA(input.Commit) || input.ExpectedTarget != "" && !api.CommitSHA(input.ExpectedTarget) || !api.ValidIdentity(input.ExecutionID) || !api.ValidIdentity(input.ActorExecutionID) || strings.TrimSpace(input.Decision) == "" || len(input.Members) == 0 || len(input.Members) > 256 {
		return nil, apiError(api.CodeInvalidRequest, "integration requires a target, exact commits, fixed execution, decision and complete member set")
	}
	if input.Purpose != "initialize" && input.Purpose != "assemble" && input.Purpose != "deliver" {
		return nil, apiError(api.CodeInvalidRequest, "invalid integration purpose")
	}
	if input.GroupID != "" && !board.ValidTaskGroup(input.GroupID) {
		return nil, apiError(api.CodeInvalidRequest, "invalid integration group")
	}
	if input.Purpose != "deliver" && (input.GroupID == "" || input.TargetRef != "refs/heads/group/"+input.GroupID) {
		return nil, apiError(api.CodeEvidenceIncomplete, "group initialization and assembly require that group's exact branch")
	}
	if input.Purpose == "deliver" && strings.HasPrefix(input.TargetRef, "refs/heads/group/") {
		return nil, apiError(api.CodeEvidenceIncomplete, "group branch writes require its own coordinator authority")
	}
	if input.Purpose == "initialize" {
		if input.Commit != input.BaseCommit || input.ExpectedTarget != "" && input.ExpectedTarget != input.Commit {
			return nil, apiError(api.CodeEvidenceIncomplete, "initialization only publishes or adopts the exact anchor")
		}
	} else if input.ExpectedTarget == "" || input.BaseCommit != input.ExpectedTarget {
		return nil, apiError(api.CodeEvidenceIncomplete, "integration must bind the observed existing target as its fast-forward base")
	}
	ids := make([]string, 0, len(input.Members))
	for _, member := range input.Members {
		if canonical, err := board.NormalizeTaskID(member.TaskID); err != nil || canonical != member.TaskID || member.Revision == 0 || member.DeliveryCommit != "" && !api.CommitSHA(member.DeliveryCommit) {
			return nil, apiError(api.CodeInvalidRequest, "invalid integration member")
		}
		ids = append(ids, member.TaskID)
	}
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		return nil, apiError(api.CodeInvalidRequest, "duplicate integration member")
	}
	if input.GroupID == "" && len(ids) != 1 {
		return nil, apiError(api.CodeEvidenceIncomplete, "several members require coordinator authority")
	}
	return ids, nil
}

func normalizedIntegrationEdges(edges []api.IntegrationEdge) []api.IntegrationEdge {
	value := slices.Clone(edges)
	slices.SortFunc(value, func(a, b api.IntegrationEdge) int {
		return cmp.Or(strings.Compare(a.Ancestor, b.Ancestor), strings.Compare(a.Descendant, b.Descendant))
	})
	return slices.Compact(value)
}

func (p *project) integrationFacts(tx *board.Transaction, input api.IntegrationRequest, exactVersions bool) error {
	ids, err := integrationIDs(input)
	if err != nil {
		return err
	}
	if input.GroupID != "" {
		if err := board.IntegrationGroupMembers(tx, input.GroupID, ids); err != nil {
			return err
		}
	}
	edges := []api.IntegrationEdge{{Ancestor: input.BaseCommit, Descendant: input.Commit}}
	if input.AlreadyContained {
		if input.ExpectedTarget == "" {
			return apiError(api.CodeEvidenceIncomplete, "contained-source reconciliation requires an existing target")
		}
		edges = []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.ExpectedTarget}}
	}
	contributions := 0
	for _, member := range input.Members {
		snapshot, err := tx.Snapshot(member.TaskID)
		if err != nil {
			return err
		}
		if board.TaskGroupFrom(snapshot.Text) != input.GroupID {
			return apiError(api.CodeEvidenceIncomplete, "integration member belongs to another group")
		}
		claim, found, err := p.readClaim(member.TaskID)
		if err != nil {
			return err
		}
		binds := exactVersions || input.Purpose == "deliver" || member.DeliveryCommit != ""
		if binds && (snapshot.Revision != member.Revision || claim.Generation != member.Generation) {
			return apiError(api.CodeRevisionConflict, "integration member version or execution changed")
		}
		if input.Purpose == "initialize" {
			if member.DeliveryCommit != "" {
				return apiError(api.CodeEvidenceIncomplete, "initialization is not a task delivery")
			}
			continue
		}
		if input.Purpose == "deliver" {
			progress, err := board.IntegrationReview(tx, member.TaskID)
			if err != nil {
				return err
			}
			if progress.Status == "excluded" {
				continue
			}
			if progress.Status != "closed" || progress.PlanTarget != input.Commit {
				return apiError(api.CodeEvidenceIncomplete, "integration requires the current closed review target for every remaining member")
			}
			if member.DeliveryCommit == "" {
				return apiError(api.CodeEvidenceIncomplete, "a remaining member has no frozen delivery")
			}
		}
		if member.DeliveryCommit == "" {
			continue
		}
		if !claimedState(snapshot.Entry.State) || snapshot.Entry.State != "review" || !found || claim.Status != api.ClaimActive || !p.server.now().Before(claim.ExpiresAt) {
			return apiError(api.CodeLeaseInvalid, "contributor must have a live review execution")
		}
		delivery, err := p.latestDelivery(member.TaskID)
		if err != nil {
			return err
		}
		if delivery == nil || delivery.Purpose == "baseline" || delivery.Commit != member.DeliveryCommit || delivery.Generation != member.Generation || delivery.ExecutionID != claim.ExecutionID {
			return apiError(api.CodeEvidenceIncomplete, "contributor does not match its latest published delivery")
		}
		contributions++
		edges = append(edges, api.IntegrationEdge{Ancestor: member.DeliveryCommit, Descendant: input.Commit})
	}
	if input.Purpose != "initialize" && contributions == 0 {
		return apiError(api.CodeEvidenceIncomplete, "integration needs at least one delivered member")
	}
	if !reflect.DeepEqual(normalizedIntegrationEdges(edges), normalizedIntegrationEdges(input.Edges)) {
		return apiError(api.CodeEvidenceIncomplete, "integration lacks the exact client-verified ancestry edges")
	}
	return nil
}

func (p *project) integrationActor(input api.IntegrationRequest, h holder) (claimRecord, error) {
	if input.GroupID != "" {
		return p.requireGroup(input.GroupID, h)
	}
	return p.requireHolder(input.Members[0].TaskID, h)
}
func (p *project) currentIntegrationActor(value api.Integration) error {
	var claim claimRecord
	var found bool
	var err error
	if value.Request.GroupID != "" {
		claim, found, err = p.readGroupClaim(value.Request.GroupID)
	} else {
		claim, found, err = p.readClaim(value.Request.Members[0].TaskID)
	}
	if err != nil {
		return err
	}
	if !found || claim.Status != api.ClaimActive || claim.Generation != value.AuthorityGeneration || claim.ExecutionID != value.Request.ActorExecutionID || claim.ClientID != value.ClientID || !p.server.now().Before(claim.ExpiresAt) {
		return apiError(api.CodeLeaseInvalid, "integration actor lost its task or coordinator authority")
	}
	return nil
}
