package server_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

func TestIntegrationRefusalNamesTheMemberWhoseReviewIsNotClosed(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	commit := strings.Repeat("b", 40)
	member, grant := integrationTaskPlan(t, f, p, "host-a", "integrate-unsealed", commit, commit, false)
	input := integrationInput(t, p, member, grant.Claim.ExecutionID, "unsealed-review")
	_, err := p.ClaimIntegration(t.Context(), "unsealed-review-target", input, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.BoardCode == "" {
		t.Fatalf("an unsealed plan was not refused by the review gate: %v", err)
	}
	if !strings.HasPrefix(refusal.Message, "member "+member.TaskID+": ") || !strings.Contains(refusal.Message, "unclosed review batches: unsealed-plan") {
		t.Fatalf("refusal %q does not name the member before the board reason", refusal.Message)
	}
	if refusal.Code != api.CodeBoardRejected || refusal.BoardCode != "board.review_evidence_invalid" {
		t.Fatalf("the prefix changed the code: %s / %s", refusal.Code, refusal.BoardCode)
	}
}

func TestIntegrationRefusalStatesTheClosedTargetAndTheRequestedCommit(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	delivery, closed := strings.Repeat("c", 40), strings.Repeat("d", 40)
	member, grant := integrationTaskAt(t, f, p, "host-a", "integrate-mismatch", delivery, closed)
	input := integrationInput(t, p, member, grant.Claim.ExecutionID, "mismatched-target")
	_, err := p.ClaimIntegration(t.Context(), "mismatched-closed-target", input, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	expectCode(t, err, api.CodeEvidenceIncomplete)
	want := "integration requires the closed review target " + closed + " of member " + member.TaskID + "; the request names " + delivery
	if !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "review status") {
		t.Fatalf("refusal %q, want %q", err, want)
	}
}
