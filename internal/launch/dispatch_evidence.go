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

// integrationTargetOfTask reads the target field of a card from the board.
func integrationTargetOfTask(root, taskID string) (string, error) {
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		return "", err
	}
	return board.IntegrationTargetOf(s.Text)
}

// verifyIntegrationTarget verifies an integration into the branch a card
// targets: its target field, or the mainline branch when target is empty. It
// proves only the one ref the evidence names; the other side of a local and
// origin pair may not exist, as in a local-only integration.
func verifyIntegrationTarget(ctx context.Context, g board.DispatchIntegration, target string) error {
	return checkIntegrationTarget(ctx, g, target, false)
}

// Before publication the candidate must contain the current tip. After publication
// the observed tip must contain the candidate. Both paths verify the same mapping.
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

// PrepareBoundDispatch verifies Git before the board's pure structural producer.
// No Git operation changes a branch, deletes a worktree or grants takeover.
func PrepareBoundDispatch(root string, in board.DispatchInput) (board.Dispatch, error) {
	if in.ID == "" {
		id, err := board.NewDispatchID()
		if err != nil {
			return board.Dispatch{}, err
		}
		in.ID = id
	}
	if w := in.Evidence.WrapUp; w != nil {
		// Do not mutate caller-owned evidence while resolving creation defaults.
		copy := *w
		in.Evidence.WrapUp = &copy
		w = &copy
		if w.Git.DispatchID == "" {
			w.Git.DispatchID = in.ID
		}
		if w.Git.TaskID == "" {
			w.Git.TaskID = in.TaskID
		}
		if w.Artifact == (board.ArtifactReference{}) {
			w.Artifact = board.ArtifactReference{TaskID: in.TaskID, Path: "dispatches/" + in.ID + "/integration.json"}
		}
		previous, err := board.ReadDispatch(root, in.TaskID, in.ID)
		if err == nil && previous.Input.Evidence.WrapUp != nil {
			if w.Git.ReviewBase == "" {
				w.Git.ReviewBase = previous.Input.Evidence.WrapUp.Git.ReviewBase
			}
			if w.Git.ReviewTarget == "" {
				w.Git.ReviewTarget = previous.Input.Evidence.WrapUp.Git.ReviewTarget
			}
			if w.Git.VerifiedAt.IsZero() {
				w.Git.VerifiedAt = previous.Input.Evidence.WrapUp.Git.VerifiedAt
			}
			return board.PrepareDispatch(root, in)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return board.Dispatch{}, err
		} else {
			if w.Git.ReviewTarget == "" || w.Git.ReviewBase == "" {
				reviewRange, e := board.DispatchReviewRange(root, in.TaskID)
				if e != nil {
					return board.Dispatch{}, e
				}
				if w.Git.ReviewTarget == "" {
					w.Git.ReviewTarget = reviewRange.Descendant
				}
				if w.Git.ReviewBase == "" {
					w.Git.ReviewBase = reviewRange.Ancestor
				}
			}
			target, err := integrationTargetOfTask(root, in.TaskID)
			if err != nil {
				return board.Dispatch{}, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := verifyIntegrationTarget(ctx, w.Git, target); err != nil {
				return board.Dispatch{}, err
			}
			w.Git.VerifiedAt = time.Now().UTC()
		}
	}
	return board.PrepareDispatch(root, in)
}

// ValidateActionEvidence runs before observation, launch and transmission.
func ValidateActionEvidence(ctx context.Context, root string, d board.Dispatch) error {
	if d.WrapUpAuthority != nil {
		return launchError("board.dispatch_evidence_invalid", "wrap-up-only grant cannot launch or deliver")
	}
	if err := board.ValidateDispatchEvidence(root, d.Input.TaskID, d.Input.ID); err != nil {
		return err
	}
	if w := d.Input.Evidence.WrapUp; w != nil {
		target, err := integrationTargetOfTask(root, d.Input.TaskID)
		if err != nil {
			return err
		}
		return verifyIntegrationTarget(ctx, w.Git, target)
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

// AuthorizeWrapUp never treats absent SESSION, nonzero transport or expiry as
// exit. A prior authorized reclaim still needs a fresh stopped observation.
func AuthorizeWrapUp(ctx context.Context, root string, r WrapUpRequest) (result board.Dispatch, err error) {
	if r.DispatchID == "" || r.TaskID == "" || strings.TrimSpace(r.Author) == "" || strings.TrimSpace(r.Reason) == "" {
		return result, launchError("board.dispatch_evidence_invalid", "wrap-up task, dispatch ID, author and reason required")
	}
	if r.Intent != nil {
		if r.Intent.Kind != "wrap-up" || r.Intent.TaskID != r.TaskID || r.Intent.ID != r.DispatchID {
			return result, launchError("board.dispatch_evidence_invalid", "wrap-up intent identity")
		}
		result, err = PrepareBoundDispatch(root, *r.Intent)
		if err != nil {
			return
		}
		if r.ExpectedRevision == 0 {
			r.ExpectedRevision = result.Revision
		}
	}
	err = board.WithDispatchDelivery(root, r.DispatchID, func() error {
		d, e := board.ReadDispatch(root, r.TaskID, r.DispatchID)
		if e != nil {
			return e
		}
		// Reconcile before probing; no new epoch is issued for an existing grant.
		if d.WrapUpAuthority != nil && d.WrapUpAuthority.Author == r.Author && d.WrapUpAuthority.Reason == r.Reason && d.WrapUpAuthority.Exit.Decision == r.ReclaimDecision {
			result = d
			return nil
		}
		if d.State == board.DispatchCompleted {
			result = d
			return nil
		}
		if d.Input.Kind != "wrap-up" || d.Revision != r.ExpectedRevision {
			return launchError("board.dispatch_conflict", r.DispatchID)
		}
		if e = ValidateActionEvidence(ctx, root, d); e != nil {
			return e
		}
		s, e := board.ReadExecutionSnapshot(root, r.TaskID, d.Authorization)
		// An expired confirmation deadline does not prevent read-only reconciliation.
		if e != nil {
			s, e = board.ReadSnapshot(root, r.TaskID)
			if e != nil {
				return e
			}
		}
		cfg, e := config.Load(false)
		if e != nil {
			return e
		}
		if e = cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(s.Text)); e != nil {
			return e
		}
		exit, e := observedDispatchExit(ctx, s, r.DispatchID)
		if e != nil {
			return e
		}
		if r.ReclaimDecision != "" {
			exit.Outcome = "reclaimed"
			exit.Decision = r.ReclaimDecision
		}
		result, e = board.AuthorizeDispatchWrapUp(root, r.TaskID, r.DispatchID, d.Revision, r.Author, r.Reason, exit)
		return e
	})
	return
}

// RunDispatch adds the Git-aware and observation-aware producers to the single
// public command. The board-only runner remains available for pure operations.
func RunDispatch(args []string) int {
	if len(args) != 2 || (args[0] != "prepare" && args[0] != "authorize-wrap-up") {
		return board.RunDispatch(args)
	}
	if _, err := config.Load(false); err != nil {
		return fail(err)
	}
	root, err := board.BoardRoot()
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return fail(err)
	}
	var d board.Dispatch
	if args[0] == "prepare" {
		var in board.DispatchInput
		if err = board.DecodeReviewJSON(data, &in); err == nil {
			if in.Kind == "check-integration" {
				if !filepath.IsAbs(args[1]) {
					return fail(launchError("board.dispatch_evidence_invalid", "absolute preflight request path required"))
				}
				return runIntegrationPreflight(root, in)
			}
			d, err = PrepareBoundDispatch(root, in)
		}
	} else {
		var r WrapUpRequest
		if err = board.DecodeReviewJSON(data, &r); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			d, err = AuthorizeWrapUp(ctx, root, r)
		}
	}
	if err != nil {
		return fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(d); err != nil {
		return fail(err)
	}
	if d.WrapUpAuthority != nil && d.State == board.DispatchPrepared {
		fmt.Fprintln(os.Stderr, t("launch.dispatch_wrap_up_only", d.WrapUpAuthority.Author, d.Input.ID, d.Authorization.Epoch))
	}
	return 0
}

// runIntegrationPreflight reads the sealed plan and Git objects without creating
// a dispatch. A passing candidate still needs the post-publication checks.
func runIntegrationPreflight(root string, in board.DispatchInput) int {
	if in.Evidence.WrapUp == nil {
		return fail(launchError("board.dispatch_evidence_invalid", "wrap_up.git required"))
	}
	g := in.Evidence.WrapUp.Git
	r, err := board.DispatchReviewRange(root, in.TaskID)
	if err != nil {
		return fail(err)
	}
	if (g.ReviewBase != "" && g.ReviewBase != r.Ancestor) || (g.ReviewTarget != "" && g.ReviewTarget != r.Descendant) {
		return fail(launchError("board.dispatch_evidence_invalid", "candidate range differs from the closed plan"))
	}
	g.ReviewBase, g.ReviewTarget = r.Ancestor, r.Descendant
	target, err := integrationTargetOfTask(root, in.TaskID)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := checkIntegrationTarget(ctx, g, target, true); err != nil {
		return fail(err)
	}
	result := map[string]string{
		"task_id": in.TaskID, "review_base": g.ReviewBase, "review_target": g.ReviewTarget,
		"source_commit": g.SourceCommit, "target_commit": g.TargetCommit, "target_ref": g.TargetRef,
	}
	if g.RebasedBase != "" {
		result["rebased_base"] = g.RebasedBase
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fail(err)
	}
	return 0
}
