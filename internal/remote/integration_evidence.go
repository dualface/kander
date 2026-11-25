package remote

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// VerifyIntegration rechecks a service receipt through the registered common
// remote. A rewound or unavailable target never passes on cached Git objects.
func (s *Session) VerifyIntegration(ctx context.Context, dir string, value api.Integration) error {
	_, err := s.verifyIntegration(ctx, dir, value)
	return err
}

// WrapUpWitness is what one full verification of wrap-up evidence observed:
// the target ref, the head origin served for it, and the commits that head
// contained.
type WrapUpWitness struct {
	targetRef string
	head      string
	kept      []string
}

func (s *Session) verifyIntegration(ctx context.Context, dir string, value api.Integration) (WrapUpWitness, error) {
	if value.Phase != "integrated" || value.Status != api.ClaimReleased || value.Request.Purpose != "deliver" || value.ID != api.IntegrationID(value.Request.TargetRef) || !api.CommitSHA(value.Request.Commit) || !api.CommitSHA(value.ObservedTarget) {
		return WrapUpWitness{}, integrationFailure("remote.integration_recovery_required")
	}
	if err := s.fetchIntegrationSource(ctx, dir, value); err != nil {
		return WrapUpWitness{}, err
	}
	for _, member := range value.Request.Members {
		if member.DeliveryCommit != "" {
			if _, err := s.FetchDelivery(ctx, dir, member.TaskID, member.DeliveryCommit); err != nil {
				return WrapUpWitness{}, err
			}
		}
	}
	kept := []string{value.Request.ConfirmAncestor(), value.ObservedTarget}
	switch value.Request.EffectiveContainment() {
	case api.IntegrationVia:
		kept = append(kept, value.Request.ViaCommit)
	case api.IntegrationReplayed:
		replay := value.Request.Replay
		if replay == nil || replay.ReviewTarget != value.Request.Commit {
			return WrapUpWitness{}, integrationFailure("remote.integration_recovery_required")
		}
		if err := s.verifyReplay(ctx, dir, *replay); err != nil {
			return WrapUpWitness{}, err
		}
	}
	for _, commit := range kept {
		if err := s.FetchCommit(ctx, dir, commit); err != nil {
			return WrapUpWitness{}, err
		}
	}
	head, err := s.integrationTarget(ctx, dir, value.Request.TargetRef)
	if err != nil {
		return WrapUpWitness{}, err
	}
	if err := s.targetContains(ctx, dir, head, kept); err != nil {
		return WrapUpWitness{}, err
	}
	return WrapUpWitness{targetRef: value.Request.TargetRef, head: head, kept: kept}, nil
}

// targetContains proves that the target head origin served contains every kept
// commit.
func (s *Session) targetContains(ctx context.Context, dir, head string, kept []string) error {
	for _, source := range kept {
		contained, err := s.integrationContains(ctx, dir, source, head)
		if err != nil {
			return err
		}
		if !contained {
			if head == "" {
				return integrationFailure("remote.integration_recovery_required")
			}
			return fmt.Errorf("git merge-base --is-ancestor %s %s: %w", source, head, integrationFailure("remote.integration_recovery_required"))
		}
	}
	return nil
}

// VerifyWrapUpIntegration binds portable dispatch evidence to the immutable
// service execution, then checks actual Git facts in this caller's clone.
func (s *Session) VerifyWrapUpIntegration(ctx context.Context, dir string, g board.DispatchIntegration) error {
	_, err := s.VerifyWrapUpIntegrationWitness(ctx, dir, g)
	return err
}

// VerifyWrapUpIntegrationWitness is VerifyWrapUpIntegration that also returns
// what it observed, so a caller can confirm the target later with
// RecheckWrapUpTarget instead of a second full verification.
func (s *Session) VerifyWrapUpIntegrationWitness(ctx context.Context, dir string, g board.DispatchIntegration) (WrapUpWitness, error) {
	// Missing identities are an input error, not an unresolved integration:
	// name them before any service read so the caller can copy the receipt.
	if missing := missingWrapUpReceiptFields(g); len(missing) != 0 {
		return WrapUpWitness{}, wrapUpEvidenceError("remote.wrap_up_receipt_fields_missing", strings.Join(missing, ", "), g.TargetRef)
	}
	if g.CWD != "project:"+s.Info.ID {
		return WrapUpWitness{}, wrapUpEvidenceError("remote.wrap_up_receipt_mismatch", "cwd=project:"+s.Info.ID, g.TargetRef)
	}
	value, err := s.Project.IntegrationExecution(ctx, g.IntegrationID, g.IntegrationExecution)
	if err != nil {
		return WrapUpWitness{}, err
	}
	if differences := wrapUpReceiptDifferences(g, value); len(differences) != 0 {
		return WrapUpWitness{}, wrapUpEvidenceError("remote.wrap_up_receipt_mismatch", strings.Join(differences, ", "), value.Request.TargetRef)
	}
	if g.ReviewTarget != g.SourceCommit || g.RebasedBase != "" {
		return WrapUpWitness{}, integrationFailure("remote.integration_members_changed")
	}
	witness, err := s.verifyIntegration(ctx, dir, value)
	if err != nil {
		return WrapUpWitness{}, err
	}
	if err := s.FetchCommit(ctx, dir, g.ReviewBase); err != nil {
		return WrapUpWitness{}, err
	}
	ancestor, err := gitAncestor(ctx, dir, g.ReviewBase, g.ReviewTarget)
	if err != nil {
		return WrapUpWitness{}, err
	}
	if !ancestor {
		return WrapUpWitness{}, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", g.ReviewBase, g.ReviewTarget, integrationFailure("remote.integration_members_changed"))
	}
	return witness, nil
}

// RecheckWrapUpTarget confirms that the target a witness observed still
// contains the verified commits. It asks origin for the target head again. An
// unchanged head already contains them; a head that moved is fetched and
// tested, so a rewound target fails as it does in a full verification.
func (s *Session) RecheckWrapUpTarget(ctx context.Context, dir string, witness WrapUpWitness) error {
	if witness.head == "" {
		return integrationFailure("remote.integration_recovery_required")
	}
	head, err := s.integrationTarget(ctx, dir, witness.targetRef)
	if err != nil {
		return err
	}
	if head == witness.head {
		return nil
	}
	return s.targetContains(ctx, dir, head, witness.kept)
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
