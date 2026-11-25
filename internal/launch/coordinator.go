package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
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
	if err := verifyIntegrationTarget(ctx, g, facts.IntegrationTarget); err != nil {
		return err
	}
	for _, c := range facts.Closures {
		if err := review.VerifyClosedReviewGit(ctx, g.CWD, c); err != nil {
			return err
		}
	}
	return verifyIntegrationTarget(ctx, g, facts.IntegrationTarget)
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
		if command.JSON() {
			command.Notify("reconcile_blocked", err.Error())
		} else {
			fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		}
	case err != nil:
		return fail(err)
	case len(c.BlockedMembers()) > 0:
		output.Result, status = reconcilePartial, reconcileExitPartial
	}
	return command.ObjectStatus(output, status)
}

// releaseSyntax is the confirmed form of the release command. It is not part of the localized
// text, so a translation cannot change the syntax, and it does not come from the installed
// contract, which stays old until the rules are refreshed.
const releaseSyntax = "kander coordinator release --end-execution <absolute-release.json>"

// leadingFlag is a switch that must come before the positional arguments of its command: a
// positional argument already read makes it a usage error. flags is the set the flag is
// registered on, whose Args holds the positional arguments read so far.
type leadingFlag struct {
	flags *pflag.FlagSet
	given bool
}

func (f *leadingFlag) String() string { return strconv.FormatBool(f.given) }
func (f *leadingFlag) Type() string   { return "bool" }

func (f *leadingFlag) Set(value string) error {
	given, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if len(f.flags.Args()) > 0 {
		return command.Usage("remote.coordinator_end_execution_position")
	}
	f.given = given
	return nil
}

// NewCoordinatorCommand builds kander coordinator: checkpoint operations and explicitly
// requested, leased Git integration against the server. Subscription events remain read-only
// wakeups.
func NewCoordinatorCommand() *cobra.Command {
	sub := func(action, argument string, valid func(string) error) *cobra.Command {
		return command.RequireProject(&cobra.Command{
			Use: action + " " + argument,
			Args: func(c *cobra.Command, args []string) error {
				if err := command.Args(1, 1)(c, args); err != nil {
					return err
				}
				return valid(args[0])
			},
			RunE: func(_ *cobra.Command, args []string) error { return command.Exit(runCoordinator(action, args[0])) },
		})
	}
	anything := func(string) error { return nil }
	absolute := func(path string) error {
		if !filepath.IsAbs(path) {
			return command.Usage("cli.error_absolute_path", path)
		}
		return nil
	}
	// Release ends the calling execution for good, so it takes an explicit confirmation. The
	// flag stands directly after the subcommand and before the path; after the path it is a
	// usage error. Without it nothing runs and no request is sent, and the message spells
	// the confirmed syntax for a coordinator that still reads an older copy of the rules. The
	// check is an argument check, so it runs before the project binding.
	var endExecution leadingFlag
	release := sub("release", "<release.json>", func(path string) error {
		if !endExecution.given {
			return command.UsageText(config.Text("remote.coordinator_release_needs_end_execution") + "\n" + releaseSyntax)
		}
		return absolute(path)
	})
	endExecution.flags = release.Flags()
	release.Flags().Var(&endExecution, "end-execution", "")
	release.Flags().Lookup("end-execution").NoOptDefVal = "true"
	return command.Group("coordinator",
		sub("show", "<group-id>", anything),
		sub("claim", "<claim.json>", absolute),
		sub("reconcile", "<observations.json>", absolute),
		sub("recover", "<request.json>", absolute),
		sub("remove-member", "<request.json>", absolute),
		release,
		sub("integrate", "<integration.json>", absolute),
		sub("integration-show", "<target-ref>", func(ref string) error {
			if !api.IntegrationTarget(ref) {
				return command.Usage("cli.error_invalid_value", "<target-ref>", ref)
			}
			return nil
		}),
	)
}

func runCoordinator(action, argument string) int {
	args := []string{action, argument}
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	if action == "integrate" || action == "integration-show" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return runRemoteIntegration(ctx, cfg, args)
	}
	if err = cfg.Rules.CheckTaskGroup("coordinator"); err != nil {
		return fail(err)
	}
	return runRemoteCoordinator(context.Background(), cfg, args)
}
