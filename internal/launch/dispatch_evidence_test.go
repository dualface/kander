package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// useGitBranchMode isolates the scope config the integration verifier reads.
func useGitBranchMode(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Git.BranchMode = mode
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

func integrationGit(t *testing.T) (string, string) {
	t.Helper()
	useGitBranchMode(t, config.GitBranchModeMainDevelop)
	cwd := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "develop")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	return cwd, git("rev-parse", "HEAD")
}

func TestDispatchIntegrationUsesActualGitAncestry(t *testing.T) {
	cwd, head := integrationGit(t)
	g := board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: "refs/heads/develop"}
	if err := verifyDispatchIntegration(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	other, foreign := integrationGit(t)
	cmd := exec.Command("git", "-C", other, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "unrelated")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	data, err := exec.Command("git", "-C", other, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	foreign = strings.TrimSpace(string(data))
	g.SourceCommit = foreign
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("foreign source claimed as integrated")
	}
	g.SourceCommit = head
	g.TargetRef = "refs/heads/task"
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("task ref substituted for develop")
	}
	g.TargetRef = "refs/heads/develop"
	g.TargetCommit = strings.Repeat("a", 40)
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("unavailable target claimed as integrated")
	}
}

func TestDispatchIntegrationTargetsConfiguredMainline(t *testing.T) {
	cwd, head := integrationGit(t)
	cmd := exec.Command("git", "-C", cwd, "update-ref", "refs/remotes/origin/main", head)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	if data, err := exec.Command("git", "-C", cwd, "branch", "main", head).CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	if data, err := exec.Command("git", "-C", cwd, "update-ref", "refs/remotes/origin/develop", head).CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	verify := func(ref string) error {
		g := board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: ref}
		return verifyDispatchIntegration(context.Background(), g)
	}
	modes := []struct {
		mode     string
		accepted []string
		rejected []string
	}{
		{config.GitBranchModeMainDevelop, []string{"refs/heads/develop", "refs/remotes/origin/develop"}, []string{"refs/heads/main", "refs/remotes/origin/main"}},
		{config.GitBranchModeMainOnly, []string{"refs/heads/main", "refs/remotes/origin/main"}, []string{"refs/heads/develop", "refs/remotes/origin/develop"}},
	}
	for _, m := range modes {
		useGitBranchMode(t, m.mode)
		for _, ref := range m.accepted {
			if err := verify(ref); err != nil {
				t.Fatalf("%s rejected %s: %v", m.mode, ref, err)
			}
		}
		for _, ref := range m.rejected {
			if err := verify(ref); err == nil {
				t.Fatalf("%s accepted %s", m.mode, ref)
			}
		}
	}
}

// The branch mode of the integration checkout decides, even when this process
// runs in another repository whose overlay selects the other mode.
func TestDispatchIntegrationReadsOverlayOfIntegrationCheckout(t *testing.T) {
	cwd, head := integrationGit(t)
	if data, err := exec.Command("git", "-C", cwd, "branch", "main", head).CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	if err := os.WriteFile(filepath.Join(cwd, config.OverlayFilename), []byte(`{"git":{"branch_mode":"main-only"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	g := board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: "refs/heads/main"}
	if err := verifyDispatchIntegration(context.Background(), g); err != nil {
		t.Fatalf("overlay of the integration checkout ignored: %v", err)
	}
	g.TargetRef = "refs/heads/develop"
	if err := verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("develop accepted although the checkout overlay selects main-only")
	}
}

func launchWrapFixture(t *testing.T) (string, string, string, board.DispatchInput) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake terminal observation; Git-only test runs natively")
	}
	root, _, bin := setupBoard(t)
	task, path := makeTodo(t, root, "wrap-binding")
	startThenReview(t, root, "claude", task, path)
	return root, task, bin, closedWrapUp(t, root, task)
}

// closedWrapUp closes an N/A review of a card in review and returns its wrap-up.
func closedWrapUp(t *testing.T, root, task string) board.DispatchInput {
	t.Helper()
	cwd, head := integrationGit(t)
	roles := map[string]string{"PMQA": "N/A: lifecycle fixture", "Security": "N/A: fixture"}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "wrap-plan", Author: "fixture", Basis: "lifecycle fixture", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "wrap-batch", TaskIDs: []string{task}, Base: head, TargetCommit: head, Requirements: roles}}}
	if err := board.CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(root, "wrap-batch")
	if err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "wrap-batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, request, board.ReviewGitEvidence{CWD: cwd, Head: head, Edges: edges, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	in := board.DispatchInput{ID: "launch-wrap", TaskID: task, Kind: "wrap-up", Message: "只清理和记录", Base: head, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "本地 develop 实际祖先验证"}}}}
	return in
}
