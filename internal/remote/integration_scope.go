package remote

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// IntegrationOptions is one intentionally named operation. Retrying its ID
// keeps the original source and actor; it never borrows another execution.
type IntegrationOptions struct {
	OperationID  string   `json:"operation_id"`
	Purpose      string   `json:"purpose"`
	TargetRef    string   `json:"target_ref"`
	Commit       string   `json:"commit"`
	GroupID      string   `json:"group_id,omitempty"`
	TaskID       string   `json:"task_id,omitempty"`
	Contributors []string `json:"contributors,omitempty"`
	Decision     string   `json:"decision"`
}

func integrationFailure(id string) error { return errors.New(config.Text(id)) }
func validateIntegrationOptions(options IntegrationOptions) error {
	if !api.ValidIdentity(options.OperationID) || !api.IntegrationTarget(options.TargetRef) || !api.CommitSHA(options.Commit) || strings.TrimSpace(options.Decision) == "" {
		return integrationFailure("remote.integration_input_required")
	}
	if options.Purpose != "initialize" && options.Purpose != "assemble" && options.Purpose != "deliver" {
		return integrationFailure("remote.integration_input_required")
	}
	if options.GroupID == "" {
		id, err := board.NormalizeTaskID(options.TaskID)
		if err != nil || id != options.TaskID || options.Purpose != "deliver" || len(options.Contributors) != 0 {
			return integrationFailure("remote.integration_input_required")
		}
	} else if !board.ValidTaskGroup(options.GroupID) || options.TaskID != "" {
		return integrationFailure("remote.integration_input_required")
	}
	if options.Purpose == "assemble" && len(options.Contributors) == 0 {
		return integrationFailure("remote.integration_input_required")
	}
	if options.Purpose != "assemble" && len(options.Contributors) != 0 {
		return integrationFailure("remote.integration_input_required")
	}
	return nil
}
func (s *Session) integrationRole(options IntegrationOptions) (Credential, error) {
	var value Credential
	var found bool
	var err error
	if options.GroupID != "" {
		value, found, err = s.CoordinatorCredential(options.GroupID)
	} else {
		value, found, err = s.Credential(options.TaskID)
	}
	if err != nil {
		return value, err
	}
	if !found || value.Lost || options.GroupID != "" && value.ResourceKind != "group" || options.GroupID == "" && value.ResourceKind != "" {
		return value, integrationFailure("remote.execution_holder_required")
	}
	return value, nil
}
func (s *Session) integrationMembers(ctx context.Context, options IntegrationOptions) ([]api.IntegrationMember, error) {
	ids := []string{options.TaskID}
	if options.GroupID != "" {
		snapshot, err := s.Project.Snapshot(ctx, api.SnapshotQuery{Groups: []string{options.GroupID}})
		if err != nil {
			return nil, err
		}
		view, err := board.RestoreView(snapshot.View)
		if err != nil {
			return nil, err
		}
		membership := view.GroupMembership()
		if err := membership.ErrFor(options.GroupID); err != nil {
			return nil, err
		}
		ids = membership.Groups[options.GroupID]
	}
	if len(ids) == 0 {
		return nil, integrationFailure("remote.integration_members_changed")
	}
	remaining, err := s.Project.ReviewRemaining(ctx, ids)
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, id := range options.Contributors {
		if !slices.Contains(ids, id) || selected[id] {
			return nil, integrationFailure("remote.integration_members_changed")
		}
		selected[id] = true
	}
	var members []api.IntegrationMember
	for _, id := range slices.Sorted(slices.Values(ids)) {
		task, err := s.Project.Show(ctx, id)
		if err != nil {
			return nil, err
		}
		if board.TaskGroupFrom(task.Text) != options.GroupID {
			return nil, integrationFailure("remote.integration_members_changed")
		}
		member := api.IntegrationMember{TaskID: task.TaskID, Revision: task.Revision}
		if task.Claim != nil {
			member.Generation = task.Claim.Generation
		}
		contributes := selected[id] || options.Purpose == "deliver" && slices.Contains(remaining, id)
		if contributes {
			if task.State != "review" || task.Claim == nil || task.Claim.Status != api.ClaimActive || task.Delivery == nil || task.Delivery.Purpose == "baseline" || task.Delivery.Generation != task.Claim.Generation || task.Delivery.ExecutionID != task.Claim.ExecutionID {
				return nil, integrationFailure("remote.integration_delivery_required")
			}
			member.DeliveryCommit = task.Delivery.Commit
		}
		members = append(members, member)
	}
	return members, nil
}
