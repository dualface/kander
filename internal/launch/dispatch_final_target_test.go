package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestPrepareBoundDispatchUsesAdvancedClosedFinalTarget(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "final-target")
	startThenReview(t, root, "claude", task, path)
	cwd, base := integrationGit(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, data, err)
		}
		return strings.TrimSpace(string(data))
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("feature.txt", "one\n")
	registered := func() string {
		git("add", ".")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "registered")
		return git("rev-parse", "HEAD")
	}()
	roles := map[string]string{"PMQA": "N/A: lifecycle fixture", "Security": "N/A: fixture"}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "final-plan", Author: "fixture", Basis: "lifecycle fixture", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "final-batch", TaskIDs: []string{task}, Base: base, TargetCommit: registered, Requirements: roles}}}
	if err := board.CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	write("feature.txt", "two\n")
	final := func() string {
		git("add", ".")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fix advance")
		return git("rev-parse", "HEAD")
	}()
	batch, err := board.ReadReviewBatch(root, "final-batch")
	if err != nil {
		t.Fatal(err)
	}
	if err = board.AdvanceReviewBatch(root, board.ReviewBatchAdvance{BatchID: "final-batch", ExpectedRevision: batch.Revision, Advance: board.ReviewAdvance{PreviousTarget: registered, Target: final, Reason: "member fix", Deliveries: map[string]string{final: task}}}); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(root, "final-batch")
	if err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "final-batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, request, board.ReviewGitEvidence{CWD: cwd, Head: final, Edges: edges, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	reviewRange, err := board.DispatchReviewRange(root, task)
	if err != nil || reviewRange.Descendant != final {
		t.Fatalf("%+v %v", reviewRange, err)
	}
	ok := board.DispatchInput{ID: "wrap-final", TaskID: task, Kind: "wrap-up", Message: "只清理和记录", Base: final, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{CWD: cwd, SourceCommit: final, TargetCommit: final, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "binds closed final target"}}}}
	if _, err = PrepareBoundDispatch(root, ok); err != nil {
		t.Fatal(err)
	}
	stale := board.DispatchInput{ID: "wrap-stale", TaskID: task, Kind: "wrap-up", Message: "只清理和记录", Base: registered, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{CWD: cwd, SourceCommit: registered, ReviewTarget: registered, ReviewBase: base, TargetCommit: registered, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "stale registered target"}}}}
	if _, err = PrepareBoundDispatch(root, stale); err == nil || !strings.Contains(err.Error(), final) {
		t.Fatalf("stale registered target accepted: %v", err)
	}
}

func TestVerifyDispatchRebaseUsesClosedFinalTargetAfterAdvance(t *testing.T) {
	cwd, _ := integrationGit(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, data, err)
		}
		return strings.TrimSpace(string(data))
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("feature.txt", "value = 1\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base content")
	base := git("rev-parse", "HEAD")
	git("checkout", "-b", "group")
	write("feature.txt", "value = 1.5\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "registered")
	registered := git("rev-parse", "HEAD")
	write("feature.txt", "value = 2\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "closed final after advance")
	final := git("rev-parse", "HEAD")
	git("checkout", "develop")
	write("other.txt", "unrelated\n")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "parallel")
	rebasedBase := git("rev-parse", "HEAD")
	git("checkout", "group")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "rebase", "develop")
	source := git("rev-parse", "HEAD")
	git("checkout", "develop")
	git("merge", "--ff-only", "group")
	full := board.DispatchIntegration{CWD: cwd, ReviewBase: base, ReviewTarget: final, SourceCommit: source, TargetCommit: source, TargetRef: "refs/heads/develop", RebasedBase: rebasedBase}
	if err := verifyDispatchIntegration(context.Background(), full); err != nil {
		t.Fatal(err)
	}
	staleRange := full
	staleRange.ReviewTarget = registered
	if err := verifyDispatchIntegration(context.Background(), staleRange); err == nil {
		t.Fatal("rebase against registered-only range accepted")
	}
}
