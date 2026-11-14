package remote

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// VerifyIntegration rechecks a service receipt through the registered common
// remote. A rewound or unavailable target never passes on cached Git objects.
func (s *Session) VerifyIntegration(ctx context.Context, dir string, value api.Integration) error {
	if value.Phase != "integrated" || value.Status != api.ClaimReleased || value.Request.Purpose != "deliver" || value.ID != api.IntegrationID(value.Request.TargetRef) || !api.CommitSHA(value.Request.Commit) || !api.CommitSHA(value.ObservedTarget) {
		return integrationFailure("remote.integration_recovery_required")
	}
	if err := s.fetchIntegrationSource(ctx, dir, value); err != nil {
		return err
	}
	for _, member := range value.Request.Members {
		if member.DeliveryCommit != "" {
			if _, err := s.FetchDelivery(ctx, dir, member.TaskID, member.DeliveryCommit); err != nil {
				return err
			}
		}
	}
	for _, commit := range []string{value.Request.Commit, value.ObservedTarget} {
		if err := s.FetchCommit(ctx, dir, commit); err != nil {
			return err
		}
	}
	head, err := s.integrationTarget(ctx, dir, value.Request.TargetRef)
	if err != nil {
		return err
	}
	for _, source := range []string{value.Request.Commit, value.ObservedTarget} {
		contained, err := s.integrationContains(ctx, dir, source, head)
		if err != nil {
			return err
		}
		if !contained {
			return integrationFailure("remote.integration_recovery_required")
		}
	}
	return nil
}

// VerifyWrapUpIntegration binds portable dispatch evidence to the immutable
// service execution, then checks actual Git facts in this caller's clone.
func (s *Session) VerifyWrapUpIntegration(ctx context.Context, dir string, g board.DispatchIntegration) error {
	// Missing identities are an input error, not an unresolved integration:
	// name them before any service read so the caller can copy the receipt.
	if missing := missingWrapUpReceiptFields(g); len(missing) != 0 {
		return wrapUpEvidenceError("remote.wrap_up_receipt_fields_missing", strings.Join(missing, ", "), g.TargetRef)
	}
	if g.CWD != "project:"+s.Info.ID {
		return wrapUpEvidenceError("remote.wrap_up_receipt_mismatch", "cwd=project:"+s.Info.ID, g.TargetRef)
	}
	value, err := s.Project.IntegrationExecution(ctx, g.IntegrationID, g.IntegrationExecution)
	if err != nil {
		return err
	}
	if differences := wrapUpReceiptDifferences(g, value); len(differences) != 0 {
		return wrapUpEvidenceError("remote.wrap_up_receipt_mismatch", strings.Join(differences, ", "), value.Request.TargetRef)
	}
	if g.ReviewTarget != g.SourceCommit || g.RebasedBase != "" {
		return integrationFailure("remote.integration_members_changed")
	}
	if err := s.VerifyIntegration(ctx, dir, value); err != nil {
		return err
	}
	if err := s.FetchCommit(ctx, dir, g.ReviewBase); err != nil {
		return err
	}
	ancestor, err := gitAncestor(ctx, dir, g.ReviewBase, g.ReviewTarget)
	if err != nil {
		return err
	}
	if !ancestor {
		return integrationFailure("remote.integration_members_changed")
	}
	return nil
}

func missingWrapUpReceiptFields(g board.DispatchIntegration) []string {
	var missing []string
	if g.IntegrationID == "" {
		missing = append(missing, "integration_id")
	}
	if g.IntegrationExecution == "" {
		missing = append(missing, "integration_execution")
	}
	if g.IntegrationGeneration == 0 {
		missing = append(missing, "integration_generation")
	}
	return missing
}

// wrapUpReceiptDifferences names each evidence field that differs from the
// published execution, with the receipt value the caller has to copy.
func wrapUpReceiptDifferences(g board.DispatchIntegration, value api.Integration) []string {
	var differences []string
	for _, field := range []struct {
		name, evidence, receipt string
	}{
		{"integration_generation", strconv.FormatUint(g.IntegrationGeneration, 10), strconv.FormatUint(value.Generation, 10)},
		{"target_ref", g.TargetRef, value.Request.TargetRef},
		{"source_commit", g.SourceCommit, value.Request.Commit},
		{"target_commit", g.TargetCommit, value.ObservedTarget},
	} {
		if field.evidence != field.receipt {
			differences = append(differences, field.name+"="+field.receipt)
		}
	}
	return differences
}

// wrapUpEvidenceError keeps the receipt-mapping guidance in one catalog entry.
// An empty target ref still yields a runnable placeholder command.
func wrapUpEvidenceError(id, detail, targetRef string) error {
	if targetRef == "" {
		targetRef = "<target_ref>"
	}
	return &api.Error{Code: api.CodeEvidenceIncomplete, Message: config.Text(id, detail, targetRef)}
}
