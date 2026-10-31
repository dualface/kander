package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/review"
)

func init() { board.SetWrapUpGitVerifier(verifyWrapUpCompletion) }

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
		return verifyWrapUpGit(ctx, r.CWD, facts)
	})
}

// verifyWrapUpGit proves the integration of a wrap-up and its closed batches. cwd
// replaces the recorded worktree when it is set.
func verifyWrapUpGit(ctx context.Context, cwd string, facts board.CoordinatorGitFacts) error {
	g := facts.Integration
	if cwd != "" {
		g.CWD = cwd
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
}

// verifyWrapUpCompletion runs for kander move done. Cleanup may already have
// removed the recorded worktree, so the main worktree proves the same objects.
func verifyWrapUpCompletion(ctx context.Context, root string, facts board.CoordinatorGitFacts) error {
	cwd := ""
	if _, err := os.Stat(facts.Integration.CWD); err != nil {
		cwd = filepath.Dir(root)
	}
	return verifyWrapUpGit(ctx, cwd, facts)
}

// recoverLivenessFn observes the recorded address only, as adoption does.
var recoverLivenessFn = func(ctx context.Context, s board.Snapshot) liveness.Report {
	return liveness.ClassifyTaskLookupContext(ctx, s.Entry, s.Text, false)
}

// RecoverCoordinator refuses a reset of a member whose executor is confirmed
// running, then lets board apply the request to the card it observed. Any other
// probe result proceeds: the user decided, and unknown never counts as alive.
func RecoverCoordinator(ctx context.Context, root string, r board.CoordinatorRecover) (board.CoordinatorCheckpoint, error) {
	s, err := board.ReadSnapshot(root, r.Member)
	if err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	if s.Entry.State == "working" || s.Entry.State == "review" {
		if report := recoverLivenessFn(ctx, s); report.Status == liveness.Alive || report.Status == liveness.AliveOrphaned {
			return board.CoordinatorCheckpoint{}, launchError("launch.recover_executor_alive", s.Entry.TaskID, report.Status)
		}
	}
	return board.RecoverCoordinator(ctx, root, r, s.Revision)
}

// reconcileExitPartial is the exit status of a reconcile that blocked some
// members. The checkpoint is committed; a script must read its blocked fields.
const reconcileExitPartial = 3

// Reconcile outcomes in the JSON output.
const (
	reconcileComplete = "complete"
	reconcilePartial  = "partial"
	reconcileFailed   = "failed"
)

// reconcileOutput is the checkpoint plus the outcome of this reconcile.
type reconcileOutput struct {
	board.CoordinatorCheckpoint
	Result string `json:"result"`
}

// finishReconcile prints the outcome of a reconcile and returns its exit status. A
// refused request has no checkpoint to print and ends as any other failure.
func finishReconcile(c board.CoordinatorCheckpoint, err error) int {
	var blocked *board.CoordinatorBlockedError
	output, status := reconcileOutput{CoordinatorCheckpoint: c, Result: reconcileComplete}, 0
	switch {
	case errors.As(err, &blocked):
		output.Result, status = reconcileFailed, 1
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	case err != nil:
		return fail(err)
	case len(c.BlockedMembers()) > 0:
		output.Result, status = reconcilePartial, reconcileExitPartial
	}
	if err = json.NewEncoder(os.Stdout).Encode(output); err != nil {
		return fail(err)
	}
	return status
}

func coordinatorUsageFail() int {
	fmt.Fprintln(os.Stderr, config.CommandUsage("coordinator"))
	return 2
}

// RunCoordinator is a one-shot recovery tool. Subscription events are wakeups;
// it never starts a subscriber, notifies an agent or changes a Git reference.
func RunCoordinator(args []string) int {
	if len(args) != 2 {
		return coordinatorUsageFail()
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
	case "claim", "reconcile", "recover":
		if args[0] == "recover" && !filepath.IsAbs(args[1]) {
			return coordinatorUsageFail()
		}
		var data []byte
		data, err = os.ReadFile(args[1])
		if err != nil {
			return fail(err)
		}
		switch args[0] {
		case "claim":
			var r board.CoordinatorClaim
			if err = board.DecodeReviewJSON(data, &r); err == nil {
				c, err = board.ClaimCoordinator(ctx, root, r)
			}
		case "reconcile":
			var r board.CoordinatorReconcile
			if err = board.DecodeReviewJSON(data, &r); err != nil {
				return fail(err)
			}
			c, err = ReconcileCoordinator(ctx, root, r)
			return finishReconcile(c, err)
		default:
			var r board.CoordinatorRecover
			if err = board.DecodeReviewJSON(data, &r); err == nil {
				c, err = RecoverCoordinator(ctx, root, r)
			}
		}
	default:
		return coordinatorUsageFail()
	}
	if err != nil {
		return fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(c); err != nil {
		return fail(err)
	}
	return 0
}
