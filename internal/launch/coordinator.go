package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/review"
)

func init() { board.SetWrapUpGitVerifier(verifyWrapUpCompletion) }

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
// removed the recorded worktree, and a board set by KANBAN_DIR can live outside the
// repository, so it looks for any checkout that holds the same objects. With none
// in reach it refuses: a completion without its evidence would hand a later rewrite
// of the history back to reconcile.
func verifyWrapUpCompletion(ctx context.Context, root string, facts board.CoordinatorGitFacts) error {
	working, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, cwd := range []string{facts.Integration.CWD, working, filepath.Dir(root)} {
		if exec.CommandContext(ctx, "git", "-C", cwd, "cat-file", "-e", facts.Integration.TargetCommit+"^{commit}").Run() == nil {
			return verifyWrapUpGit(ctx, cwd, facts)
		}
	}
	return launchError("launch.wrap_up_needs_checkout", facts.Integration.TaskID, facts.Integration.TargetCommit)
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

// releaseSyntax is the confirmed form of the release command. It is not part
// of the localized text, so a translation cannot change the syntax.
const releaseSyntax = "kander coordinator release --end-execution <absolute-release.json>"

func coordinatorUsageFail() int {
	fmt.Fprintln(os.Stderr, config.CommandUsage("coordinator"))
	return 2
}

// coordinatorArgs checks the shape of the arguments before any configuration is
// read, and returns the arguments without the release confirmation. Release
// ends the calling execution for good, so it takes an explicit confirmation.
// Without it nothing runs and no request is sent, and the message spells the
// new syntax for a coordinator that still reads an older copy of the rules.
// The binding check of the public entrance runs this first, so a refusal is
// not hidden behind a configuration error.
func coordinatorArgs(args []string) (rest []string, status int, refused bool) {
	confirmed := false
	if len(args) == 3 && args[0] == "release" && args[1] == "--end-execution" {
		confirmed, args = true, []string{args[0], args[2]}
	}
	if len(args) == 2 && args[0] == "release" && !confirmed && args[1] != "--end-execution" {
		fmt.Fprintln(os.Stderr, config.Text("remote.coordinator_release_needs_end_execution"))
		fmt.Fprintln(os.Stderr, releaseSyntax)
		return nil, 2, true
	}
	if len(args) != 2 || slices.Contains(args, "--end-execution") {
		return nil, coordinatorUsageFail(), true
	}
	return args, 0, false
}

// RunCoordinatorRemote runs checkpoint operations and explicitly requested,
// leased Git integration against the server. Subscription events remain
// read-only wakeups.
func RunCoordinatorRemote(args []string) int {
	args, status, refused := coordinatorArgs(args)
	if refused {
		return status
	}
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	if args[0] == "integrate" || args[0] == "integration-show" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return runRemoteIntegration(ctx, cfg, args)
	}
	if err = cfg.Rules.CheckTaskGroup("coordinator"); err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return runRemoteCoordinator(ctx, cfg, args)
}
