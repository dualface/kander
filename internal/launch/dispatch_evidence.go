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
	if !filepath.IsAbs(g.CWD) || (g.TargetRef != "refs/heads/develop" && g.TargetRef != "refs/remotes/origin/develop") {
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := verifyDispatchIntegration(ctx, w.Git); err != nil {
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
		return verifyDispatchIntegration(ctx, w.Git)
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
		start := time.Now()
		observed := DispatchObservation(ctx, s)
		if board.MetadataFrom(s.Text, board.FieldSession) == "" || !observed.ValidFor(s.Entry, s.Text) || observed.Status != liveness.Stopped || observed.ObservedAt.Before(start) {
			return launchError("launch.dispatch_recovery_unproven", r.DispatchID, observed.Detail)
		}
		outcome := "stopped"
		if r.ReclaimDecision != "" {
			outcome = "reclaimed"
		}
		exit := board.WrapUpExitEvidence{Outcome: outcome, Decision: r.ReclaimDecision, CardRevision: s.Revision, Session: observed.Identity.Session, Window: observed.Identity.Window, Owner: observed.Identity.Owner, StartedAt: observed.Identity.StartedAt, ObservedAt: observed.ObservedAt}
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
