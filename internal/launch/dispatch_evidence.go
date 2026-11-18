package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func readDispatchEvidence(path string) (board.DispatchEvidence, error) {
	var evidence board.DispatchEvidence
	if path == "" {
		return evidence, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return evidence, err
	}
	err = board.DecodeReviewJSON(data, &evidence)
	return evidence, err
}

func verifyDispatchIntegration(ctx context.Context, g board.DispatchIntegration) error {
	if !filepath.IsAbs(g.CWD) {
		return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
	}
	// The mainline branch follows the git.branch_mode in force for the checkout
	// that holds the integration, not for the directory this process runs in.
	cfg, err := config.LoadFor(g.CWD)
	if err != nil {
		return err
	}
	mainline := config.MainlineBranch(cfg)
	if g.TargetRef != "refs/heads/"+mainline && g.TargetRef != "refs/remotes/origin/"+mainline {
		return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
	}
	for _, commit := range []string{g.SourceCommit, g.TargetCommit, g.ReviewTarget, g.ReviewBase} {
		if len(commit) != 40 || strings.Trim(commit, "0123456789abcdef") != "" {
			return launchError("board.dispatch_evidence_invalid", "integration commit")
		}
	}
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.CWD}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			return "", launchError("board.dispatch_evidence_invalid", strings.TrimSpace(string(data))+": "+err.Error())
		}
		return strings.TrimSpace(string(data)), nil
	}
	head, err := git("rev-parse", "--verify", g.TargetRef+"^{commit}")
	if err != nil {
		return err
	}
	edges := [][2]string{{g.ReviewBase, g.ReviewTarget}, {g.SourceCommit, g.TargetCommit}, {g.TargetCommit, head}}
	if g.RebasedBase == "" {
		if g.SourceCommit != g.ReviewTarget {
			return launchError("board.dispatch_evidence_invalid", "source must equal the closed target or provide an explicit rebase mapping")
		}
		edges = append(edges, [2]string{g.ReviewTarget, g.SourceCommit})
	} else {
		if len(g.RebasedBase) != 40 || strings.Trim(g.RebasedBase, "0123456789abcdef") != "" {
			return launchError("board.dispatch_evidence_invalid", "rebased base")
		}
		edges = append(edges, [2]string{g.ReviewBase, g.RebasedBase}, [2]string{g.RebasedBase, g.SourceCommit})
		if err := verifyDispatchRebase(ctx, g); err != nil {
			return err
		}
	}
	for _, edge := range edges {
		if _, err = git("merge-base", "--is-ancestor", edge[0], edge[1]); err != nil {
			return err
		}
	}
	after, err := git("rev-parse", "--verify", g.TargetRef+"^{commit}")
	if err != nil {
		return err
	}
	if after != head {
		return launchError("board.dispatch_evidence_invalid", "integration target changed during verification")
	}
	return nil
}

type WrapUpRequest struct {
	TaskID           string               `json:"task_id"`
	DispatchID       string               `json:"dispatch_id"`
	ExpectedRevision uint64               `json:"expected_revision"`
	Intent           *board.DispatchInput `json:"intent,omitempty"`
	Author           string               `json:"author"`
	Reason           string               `json:"reason"`
	ReclaimDecision  string               `json:"reclaim_decision,omitempty"`
}

// RunDispatchRemote implements kander dispatch against the server.
func RunDispatchRemote(args []string) int {
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	return runRemoteDispatch(args, cfg)
}
