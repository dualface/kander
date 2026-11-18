package review

import (
	"context"
	"github.com/dualface/kander/internal/command"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// runDisposition runs one evidence command once the arguments and the
// configuration are checked. The client binds it to the server project of the
// checkout; tests bind it to a board directory.
var runDisposition = runRemoteDisposition

func runDispositionMode(action string, args []string) int {
	cfg, loadErr := config.Load(true)
	if loadErr != nil {
		return dispositionFailure(loadErr)
	}
	return runDisposition(cfg, append([]string{action}, args...))
}
func dispositionFailure(err error) int {
	if command.JSON() {
		return command.FailStatus(2, err)
	}
	userError(err.Error())
	return 2
}
func validatePlanGit(p board.ReviewPlan) error {
	for _, b := range p.Batches {
		if !board.ReviewNeedsGit(b) {
			continue
		}
		if err := verifyPlannedBatchGit(p.CWD, b); err != nil {
			return err
		}
	}
	return nil
}

// verifyPlannedBatchGit checks the Git claims of a batch that needs Git. A batch
// with no delivery commit also needs its target in the history of the repository
// at cwd now: that is the check closure repeats later, and a wrong commit shows
// up here instead of when the batch cannot close.
func verifyPlannedBatchGit(cwd string, b board.ReviewPlanBatch) error {
	if err := verifyGitEdge(cwd, board.ReviewGitEdge{Ancestor: b.Base, Descendant: b.TargetCommit}); err != nil {
		return err
	}
	if board.ReviewNoDelivery(b) {
		_, err := verifyNoDeliveryHead(cwd, b.TargetCommit)
		return err
	}
	return nil
}

// verifyRetargetGit runs the Git checks of the retarget_batches operation on the
// new base and target of each batch; board checks the plan side.
func verifyRetargetGit(cwd string, p board.ReviewPlan, x board.ReviewPlanExtension) error {
	for _, id := range slices.Sorted(maps.Keys(x.RetargetBatches)) {
		r := x.RetargetBatches[id]
		at := slices.IndexFunc(p.Batches, func(b board.ReviewPlanBatch) bool { return b.BatchID == id })
		if at < 0 {
			continue
		}
		next := board.ReviewPlanBatch{BatchID: id, Base: r.Base, TargetCommit: r.Target, Requirements: p.Batches[at].Requirements}
		if err := verifyPlannedBatchGit(cwd, next); err != nil {
			return err
		}
	}
	return nil
}
func verifyGitEdge(cwd string, e board.ReviewGitEdge) error {
	return verifyGitEdgeContext(context.Background(), cwd, e)
}

func verifyGitEdgeContext(ctx context.Context, cwd string, e board.ReviewGitEdge) error {
	for _, sha := range []string{e.Ancestor, e.Descendant} {
		if len(sha) != 40 && len(sha) != 64 || strings.Trim(sha, "0123456789abcdef") != "" {
			return archiveError("full commit SHA required")
		}
		out, _, code, err := gitCommandContext(ctx, []string{"cat-file", "-t", sha}, cwd, "")
		if err != nil {
			return err
		}
		if code != 0 || strings.TrimSpace(out) != "commit" {
			return archiveError("Git commit required: " + sha)
		}
	}
	_, _, code, err := gitCommandContext(ctx, []string{"merge-base", "--is-ancestor", e.Ancestor, e.Descendant}, cwd, "")
	if err != nil {
		return err
	}
	if code != 0 {
		return archiveError("Git ancestry: " + e.Ancestor + " / " + e.Descendant)
	}
	return nil
}
func verifyClosureHead(cwd, target string) error {
	head, _, code, err := gitCommand([]string{"rev-parse", "HEAD"}, cwd, "")
	if err != nil {
		return err
	}
	ok, status := gitStatus(cwd)
	if code != 0 || strings.TrimSpace(head) != target || !ok || status != "" {
		return archiveError("closure requires clean worktree at final target HEAD")
	}
	return nil
}

// verifyNoDeliveryHead checks that the target of a batch with no delivery commit is
// HEAD or one of its ancestors, and returns the HEAD it observed. The worktree may
// be dirty and HEAD may have moved on: no delivery means no reviewed patch to keep
// out of the integration.
func verifyNoDeliveryHead(cwd, target string) (string, error) {
	out, _, code, err := gitCommand([]string{"rev-parse", "HEAD"}, cwd, "")
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(out)
	if code != 0 || head == "" {
		return "", archiveError("Git HEAD required")
	}
	_, _, code, err = gitCommand([]string{"merge-base", "--is-ancestor", target, head}, cwd, "")
	if err != nil {
		return "", err
	}
	switch code {
	case 0:
		return head, nil
	case 1:
		return "", newGate(2, "review.no_delivery_target_not_in_head", target, head)
	default:
		return "", archiveError("Git commit required: " + target)
	}
}

// verifyBatchHead is the HEAD check of a closure. A batch with a delivery closes
// only at its exact target on a clean worktree; a batch with no delivery closes at
// any HEAD that still holds the target, and the HEAD it saw goes into the evidence.
func verifyBatchHead(cwd string, b board.ReviewBatch) (observed string, err error) {
	if board.ReviewNoDelivery(board.ReviewPlanBatch{Base: b.Base, TargetCommit: b.TargetCommit, Requirements: b.Requirements}) {
		return verifyNoDeliveryHead(cwd, b.TargetCommit)
	}
	return "", verifyClosureHead(cwd, b.TargetCommit)
}

// closureGitEvidence is the Git evidence of a closure for a batch that needs Git,
// without the checks that need the view: the target as the head, and the marker of
// a batch with no delivery.
func closureGitEvidence(cwd string, b board.ReviewBatch, mechanical []board.ReviewMechanicalGit, edges []board.ReviewGitEdge, observed string) board.ReviewGitEvidence {
	evidence := board.ReviewGitEvidence{CWD: cwd, Head: b.TargetCommit, Mechanical: mechanical, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if board.ReviewNoDelivery(board.ReviewPlanBatch{Base: b.Base, TargetCommit: b.TargetCommit, Requirements: b.Requirements}) {
		evidence.NoDelivery, evidence.ObservedHead = true, observed
	}
	return evidence
}

func hydrateIncremental(root string, options archiveOptions, arguments []string, replay bool, stores ...reviewStore) ([]string, error) {
	store := selectedReviewStore(root, stores)
	if options.previousID == "" {
		return arguments, nil
	}
	previous, err := store.Run(options.previousID)
	if err != nil {
		return nil, err
	}
	// A new run names the members that remain. A replay keeps the frozen members
	// of its own run, which the replay check has already compared.
	remaining, err := store.Remaining(previous.TaskIDs)
	if err != nil {
		return nil, err
	}
	if previous.BatchID != options.batchID || !replay && !slices.Equal(remaining, options.tasks) {
		return nil, archiveError("incremental batch/members mismatch")
	}
	if len(arguments) == 7 && arguments[6] != "" && arguments[6] != previous.Commit {
		return nil, archiveError("incremental commit mismatch")
	}
	supplement := ""
	if len(arguments) >= 6 {
		supplement = arguments[5]
	}
	var context []byte
	if replay {
		context, err = store.Input(options.runID, "review-context.md")
	} else {
		context, err = store.Incremental(options.previousID)
	}
	if err != nil {
		return nil, err
	}
	if replay {
		_, frozen, e := board.SplitReviewContext(context)
		if e != nil {
			return nil, e
		}
		if frozen != supplement {
			return nil, archiveError("incremental supplemental context replay mismatch")
		}
	} else {
		context = board.MergeReviewContext(context, supplement)
	}
	result := append([]string{}, arguments[:5]...)
	return append(result, string(context), previous.Commit), nil
}

// canonicalEvidenceCWD resolves an evidence command CWD the same way kander review resolves its
// worktree root (EvalSymlinks, Abs, Clean), so Git checks and stored evidence use one spelling of the
// directory. A path that cannot be resolved keeps its cleaned spelling and fails in the later checks.
func canonicalEvidenceCWD(cwd string) string {
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return filepath.Clean(abs)
		}
	}
	return filepath.Clean(cwd)
}
