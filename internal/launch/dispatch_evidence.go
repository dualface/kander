package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
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

// verifyDispatchIntegration verifies an integration into the mainline branch,
// the target of a card with no target field.
func verifyDispatchIntegration(ctx context.Context, g board.DispatchIntegration) error {
	return verifyIntegrationTarget(ctx, g, "")
}

// verifyIntegrationTarget verifies an integration into the branch a card
// targets: its target field, or the mainline branch when target is empty. It
// proves only the one ref the evidence names; the other side of a local and
// origin pair may not exist, as in a local-only integration.
func verifyIntegrationTarget(ctx context.Context, g board.DispatchIntegration, target string) error {
	return checkIntegrationTarget(ctx, g, target, false)
}

// The candidate contains the observed tip before publication. Afterwards,
// the observed tip must contain the candidate. The review mapping is shared.
func checkIntegrationTarget(ctx context.Context, g board.DispatchIntegration, target string, beforePublication bool) error {
	if !filepath.IsAbs(g.CWD) {
		return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
	}
	// The mainline branch follows the git.branch_mode in force for the checkout
	// that holds the integration, not for the directory this process runs in.
	cfg, err := config.LoadFor(g.CWD)
	if err != nil {
		return err
	}
	branch := config.MainlineBranch(cfg)
	if target != "" {
		// `main` only advances from `develop` in main-develop mode, so a card may
		// not name it as a target there.
		if target == "main" && branch == "develop" {
			return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
		}
		branch = target
	}
	if g.TargetRef != "refs/heads/"+branch && g.TargetRef != "refs/remotes/origin/"+branch {
		return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
	}
	if target != "" && exec.CommandContext(ctx, "git", "-C", g.CWD, "check-ref-format", "--branch", target).Run() != nil {
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
	// A card's own target is read as the exact ref path, so a same-named ref in
	// another namespace cannot stand in for a missing one.
	resolve := func() (string, error) {
		if target == "" {
			return git("rev-parse", "--verify", g.TargetRef+"^{commit}")
		}
		exact, err := git("show-ref", "--verify", "--hash", g.TargetRef)
		if err != nil {
			return "", err
		}
		return git("rev-parse", "--verify", exact+"^{commit}")
	}
	head, err := resolve()
	if err != nil {
		return err
	}
	publication := [2]string{g.TargetCommit, head}
	if beforePublication {
		publication = [2]string{head, g.TargetCommit}
	}
	edges := [][2]string{{g.ReviewBase, g.ReviewTarget}, {g.SourceCommit, g.TargetCommit}, publication}
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
			return fmt.Errorf("git merge-base --is-ancestor %s %s: %w", edge[0], edge[1], err)
		}
	}
	after, err := resolve()
	if err != nil {
		return err
	}
	if after != head {
		return launchError("board.dispatch_evidence_invalid", "integration target changed during verification")
	}
	return nil
}

// verifyPortableWrapUpTarget binds a distributed receipt to a declared card
// target. For older cards the receipt itself supplies the target as before.
func verifyPortableWrapUpTarget(cwd, target, ref string) error {
	if target == "" {
		return nil
	}
	if ref != "refs/heads/"+target {
		return launchError("board.dispatch_evidence_invalid", "integration binding")
	}
	if target == "main" {
		cfg, err := config.LoadFor(cwd)
		if err != nil {
			return err
		}
		if config.MainlineBranch(cfg) == "develop" {
			return launchError("board.dispatch_evidence_invalid", "integration CWD or target ref")
		}
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

// NewDispatchCommand builds kander dispatch, the durable dispatch protocol against the server.
func NewDispatchCommand() *cobra.Command {
	var decision string
	run := func(action string) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load(false)
			if err != nil {
				return command.Exit(fail(err))
			}
			if err := remote.RequireBinding(cfg); err != nil {
				return command.Exit(fail(err))
			}
			return command.Exit(runRemoteDispatch(dispatchArguments{action: action, args: args, decision: decision}, cfg))
		}
	}
	sub := func(use string, args int) *cobra.Command {
		name, _, _ := strings.Cut(use, " ")
		return command.RequireProject(&cobra.Command{Use: use, Args: command.Args(args, args), RunE: run(name)})
	}
	end := func(use string) *cobra.Command {
		c := sub(use+" <task-id> <dispatch-id> <dispatch-revision> <reason>", 4)
		c.Flags().StringVar(&decision, "decision", "", "")
		return c
	}
	return command.Group("dispatch",
		sub("prepare <intent.json>", 1),
		sub("authorize-wrap-up <request.json>", 1),
		sub("show <task-id> <dispatch-id>", 2),
		end("fail"),
		end("cancel"),
		sub("settle <task-id> <dispatch-id> <dispatch-revision>", 3),
	)
}
