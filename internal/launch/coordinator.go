package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/review"
)

// ReconcileCoordinator verifies actual Git relations while board validates
// original review/dispatch records and commits only a fenced checkpoint.
func ReconcileCoordinator(ctx context.Context, root string, r board.CoordinatorReconcile) (board.CoordinatorCheckpoint, error) {
	return board.ReconcileCoordinator(ctx, root, r, func(ctx context.Context, facts board.CoordinatorGitFacts) error {
		if d := facts.Delivery; d != nil {
			if !filepath.IsAbs(r.CWD) || d.Branch == "main" || d.Branch == "develop" || strings.HasPrefix(d.Branch, "group/") {
				return launchError("board.coordinator_conflict", "delivery CWD or task branch")
			}
			cmd := exec.CommandContext(ctx, "git", "-C", r.CWD, "check-ref-format", "refs/heads/"+d.Branch)
			if err := cmd.Run(); err != nil {
				return err
			}
			out, err := exec.CommandContext(ctx, "git", "-C", r.CWD, "rev-parse", "--verify", "refs/heads/"+d.Branch+"^{commit}").Output()
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(out)) != d.Commit {
				return launchError("board.coordinator_conflict", "task branch head differs from delivery")
			}
			return ctx.Err()
		}
		g := facts.Integration
		if r.CWD != "" {
			g.CWD = r.CWD
		}
		if err := verifyDispatchIntegration(ctx, g); err != nil {
			return err
		}
		for _, c := range facts.Closures {
			if err := review.VerifyClosedReviewGit(ctx, g.CWD, c); err != nil {
				return err
			}
		}
		return verifyDispatchIntegration(ctx, g)
	})
}

// RunCoordinator is a one-shot recovery tool. Subscription events are wakeups;
// it never starts a subscriber, notifies an agent or changes a Git reference.
func RunCoordinator(args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(t("board.coordinator_usage"))
		return 0
	}
	if len(args) != 2 {
		return fail(launchError("board.coordinator_usage"))
	}
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err = cfg.Rules.CheckTaskGroup("coordinator"); err != nil {
		return fail(err)
	}
	root, err := board.BoardRoot()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var c board.CoordinatorCheckpoint
	switch args[0] {
	case "show":
		c, err = board.ReadCoordinatorCheckpointContext(ctx, root, args[1])
	case "claim", "reconcile":
		var data []byte
		data, err = os.ReadFile(args[1])
		if err != nil {
			return fail(err)
		}
		if args[0] == "claim" {
			var r board.CoordinatorClaim
			if err = board.DecodeReviewJSON(data, &r); err == nil {
				c, err = board.ClaimCoordinator(ctx, root, r)
			}
		} else {
			var r board.CoordinatorReconcile
			if err = board.DecodeReviewJSON(data, &r); err == nil {
				c, err = ReconcileCoordinator(ctx, root, r)
			}
		}
	default:
		return fail(launchError("board.coordinator_usage"))
	}
	if err != nil {
		return fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(c); err != nil {
		return fail(err)
	}
	return 0
}
