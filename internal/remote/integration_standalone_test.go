package remote_test

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/i18n"
)

// closedPlanFixture leaves a standalone card in working with the task branch
// filled and its review plan closed on the fixture commit, the order the
// released rules prescribe before the card moves into review.
func closedPlanFixture(t *testing.T) *integrationFixture {
	t.Helper()
	f := newIntegrationGitFixture(t)
	f.closeReviewPlan(f.claimedTask())
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *integrationFixture) moveIntoReview() api.Task {
	f.t.Helper()
	task, err := f.a.MoveTask(f.t.Context(), f.task.TaskID, api.Move{State: "review", DeliveryCommit: f.commit})
	if err != nil {
		f.t.Fatalf("move review --delivery-commit: %v", err)
	}
	if task.State != "review" || task.Delivery == nil || task.Delivery.Commit != f.commit {
		f.t.Fatalf("moved card: state %q delivery %+v", task.State, task.Delivery)
	}
	return task
}

func TestStandaloneCardIntegratesAfterMovingIntoReviewAtTheClosedTarget(t *testing.T) {
	f := closedPlanFixture(t)
	f.moveIntoReview()
	result, err := f.a.Integrate(t.Context(), f.first, f.options("standalone-review"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Commit != f.commit {
		t.Fatalf("receipt names %s, want the closed target %s", result.Request.Commit, f.commit)
	}
	f.complete(result)
}

func TestStandaloneCardInReviewReturnsToWorkingWithoutAnOwner(t *testing.T) {
	f := closedPlanFixture(t)
	f.moveIntoReview()
	before, err := f.a.Project.Show(t.Context(), f.task.TaskID)
	if err != nil || before.Claim == nil {
		t.Fatalf("claim before the return: %+v %v", before.Claim, err)
	}
	back, err := f.a.MoveTask(t.Context(), f.task.TaskID, api.Move{State: "working"})
	if err != nil {
		t.Fatalf("move working: %v", err)
	}
	if back.State != "working" || back.Claim == nil || back.Claim.ExecutionID != before.Claim.ExecutionID || back.Claim.Generation != before.Claim.Generation {
		t.Fatalf("returned card: state %q claim %+v, want the claim %+v", back.State, back.Claim, before.Claim)
	}
	check, err := f.a.Project.Check(t.Context(), api.CheckQuery{Tasks: []string{f.task.TaskID}})
	if err != nil || check.Code != 0 {
		t.Fatalf("check after the return: %+v %v", check, err)
	}
	f.moveIntoReview()
}

func TestIntegrationOfAStandaloneCardInWorkingNamesTheMoveIntoReview(t *testing.T) {
	f := closedPlanFixture(t)
	before := f.targetHead()
	_, err := f.a.Integrate(t.Context(), f.first, f.options("standalone-working"))
	if err == nil {
		t.Fatal("a card in working was integrated")
	}
	for _, want := range []string{f.task.TaskID, "working/", "kander move " + f.task.TaskID + " review --delivery-commit " + f.commit} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err, want)
		}
	}
	if after := f.targetHead(); after != before {
		t.Fatal("rejected integration moved the target")
	}
}

func TestIntegrationOfAStandaloneCardInReviewWithoutItsDeliveryNamesTheReturn(t *testing.T) {
	f := closedPlanFixture(t)
	f.moveIntoReview()
	// A baseline replaces the latest delivery, so the card in review no longer
	// has a delivery of its current execution.
	if _, err := f.a.PublishBaseline(t.Context(), f.first, f.task.TaskID, f.base, f.credential); err != nil {
		t.Fatal(err)
	}
	_, err := f.a.Integrate(t.Context(), f.first, f.options("standalone-stale"))
	if err == nil {
		t.Fatal("a card without a current delivery was integrated")
	}
	back, publish := "kander move "+f.task.TaskID+" working", "kander move "+f.task.TaskID+" review --delivery-commit "+f.commit
	at, from := strings.Index(err.Error(), back), strings.Index(err.Error(), publish)
	if at < 0 || from < at {
		t.Fatalf("refusal %q must name the return before the publication", err)
	}
}

func TestDeliveryRefusalsHaveTheSamePlaceholdersInEveryLanguage(t *testing.T) {
	const sha = "0123456789abcdef"
	for _, lang := range []string{"en", "ja", "cn"} {
		for _, text := range []struct {
			text  string
			wants []string
		}{
			{i18n.Text(lang, "remote.integration_delivery_required", "member-card", "working", sha), []string{"member-card", "working/", "kander move member-card review --delivery-commit " + sha, "review/"}},
			{i18n.Text(lang, "remote.integration_delivery_stale", "member-card", sha), []string{"member-card", "kander move member-card working", "kander move member-card review --delivery-commit " + sha, "review/"}},
		} {
			for _, want := range text.wants {
				if !strings.Contains(text.text, want) {
					t.Errorf("%s text %q lacks %q", lang, text.text, want)
				}
			}
			if strings.Contains(text.text, "{{") || strings.Contains(text.text, "%!") {
				t.Errorf("%s text keeps an unfilled placeholder: %q", lang, text.text)
			}
		}
	}
}
