package review

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func dispositionCommand(name string) bool {
	switch name {
	case "plan", "extend-plan", "advance", "assign", "disposition", "interpret", "map-legacy", "aggregate", "close", "progress":
		return true
	}
	return false
}
func runDispositionCommand(args []string) int {
	if len(args) >= 2 && args[1] == "--schema" {
		if len(args) != 2 || !schemaCommand(args[0]) {
			return dispositionFailure(newGateMsg(2, config.CommandUsage("review")))
		}
		return printEvidenceSchema(args[0])
	}
	if len(args) < 3 {
		return dispositionFailure(newGateMsg(2, config.CommandUsage("review")))
	}
	action, cwd, input := args[0], args[1], args[2]
	if !filepath.IsAbs(cwd) {
		return dispositionFailure(archiveError("absolute CWD required"))
	}
	cwd = canonicalEvidenceCWD(cwd)
	root, err := board.BoardRootAt(cwd)
	if err != nil {
		return dispositionFailure(err)
	}
	if action != "disposition" && len(args) != 3 || action == "disposition" && len(args) != 4 {
		return dispositionFailure(archiveError("command arguments"))
	}
	var result any
	switch action {
	case "plan":
		var p board.ReviewPlan
		if err = readArchiveJSON(input, &p); err == nil {
			if !filepath.IsAbs(p.CWD) || !board.SameReviewCWD(canonicalEvidenceCWD(p.CWD), cwd) {
				err = archiveError("plan CWD mismatch")
			} else {
				p.CWD = cwd
				err = validatePlanGit(p)
			}
			if err == nil {
				err = board.CreateReviewPlan(root, p)
			}
			if err == nil {
				notePinnedNotApplicable(root, p)
			}
		}
		result = map[string]string{"plan_id": p.PlanID}
	case "advance":
		var x board.ReviewBatchAdvance
		if err = readArchiveJSON(input, &x); err == nil {
			var b board.ReviewBatch
			b, err = board.ReadReviewBatch(root, x.BatchID)
			if err == nil {
				if b.PlanID == "" {
					err = archiveError("batch has no review plan; create its plan before advancing")
				} else {
					err = verifyPlanCWD(root, b.PlanID, cwd)
				}
			}
			if err == nil {
				err = verifyClosureHead(cwd, x.Advance.Target)
			}
			if err == nil {
				err = validateReviewAdvance(reviewContext{root: cwd, commit: x.Advance.Target}, x.Advance, b.TaskIDs)
			}
			if err == nil {
				err = board.AdvanceReviewBatch(root, x)
			}
		}
		result = map[string]string{"batch_id": x.BatchID}
	case "extend-plan":
		var x board.ReviewPlanExtension
		if err = readArchiveJSON(input, &x); err == nil {
			err = verifyPlanCWD(root, x.PlanID, cwd)
			if err == nil && x.ResolveBatch != nil {
				err = resolveBatchWithGit(root, cwd, x)
			} else if err == nil {
				if x.Batch != nil && board.ReviewNeedsGit(*x.Batch) {
					err = verifyPlannedBatchGit(cwd, *x.Batch)
				}
				if err == nil && len(x.RetargetBatches) > 0 {
					err = verifyRetargetGit(root, cwd, x)
				}
				if err == nil {
					err = board.ExtendReviewPlan(root, x)
				}
			}
		}
		result = map[string]string{"plan_id": x.PlanID}
	case "assign":
		var a board.ReviewAssignment
		if err = readArchiveJSON(input, &a); err == nil {
			err = board.AssignReviewFindings(root, a)
		}
		result = map[string]string{"run_id": a.RunID}
	case "disposition":
		var d board.ReviewDisposition
		revision, e := strconv.ParseUint(args[3], 10, 64)
		err = e
		if err == nil {
			err = readArchiveJSON(input, &d)
		}
		if err == nil {
			err = board.SubmitReviewDisposition(root, d, revision)
		}
		result = map[string]string{"record_id": d.RecordID}
	case "interpret":
		var m board.ReviewInterpretation
		if err = readArchiveJSON(input, &m); err == nil {
			err = board.InterpretReview(root, m)
		}
		result = map[string]string{"run_id": m.RunID}
	case "map-legacy":
		var m board.LegacyFindingMap
		if err = readArchiveJSON(input, &m); err == nil {
			err = board.MapLegacyReview(root, m)
		}
		result = map[string]string{"run_id": m.RunID}
	case "aggregate":
		result, err = board.PublishReviewDisposition(root, input)
	case "progress":
		var progress board.ReviewProgress
		progress, err = board.ReviewTaskProgress(root, input)
		result = progress
		if err != nil && (progress.PlanTarget != "" || progress.BatchTarget != "") {
			data, marshalErr := json.MarshalIndent(progress, "", "  ")
			if marshalErr == nil {
				fmt.Println(string(data))
			}
			return dispositionFailure(err)
		}
	case "close":
		var r board.ReviewCloseRequest
		if err = readArchiveJSON(input, &r); err == nil {
			result, err = closeWithGit(root, cwd, r)
		}
	}
	if err != nil {
		return dispositionFailure(err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return dispositionFailure(err)
	}
	fmt.Println(string(data))
	return 0
}
func dispositionFailure(err error) int { userError(err.Error()); return 2 }
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
// with no delivery commit also needs its target in the history of the plan CWD
// now: that is the check closure repeats later, and a wrong commit shows up here
// instead of when the batch cannot close.
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
func verifyRetargetGit(root, cwd string, x board.ReviewPlanExtension) error {
	p, err := board.ReadReviewPlan(root, x.PlanID)
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(x.RetargetBatches)) {
		r := x.RetargetBatches[id]
		at := slices.IndexFunc(p.Batches, func(b board.ReviewPlanBatch) bool { return b.BatchID == id })
		if at < 0 {
			continue
		}
		next := board.ReviewPlanBatch{BatchID: id, Base: r.Base, TargetCommit: r.Target, Requirements: p.Batches[at].Requirements}
		if err = verifyPlannedBatchGit(cwd, next); err != nil {
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

func closeWithGit(root, cwd string, r board.ReviewCloseRequest) (board.ReviewClosure, error) {
	var c board.ReviewClosure
	v, err := board.ReadReviewBatchView(root, r.BatchID)
	if err != nil {
		return c, err
	}
	planned := board.ReviewPlanBatch{Base: v.Batch.Base, TargetCommit: v.Batch.TargetCommit, Requirements: v.Batch.Requirements}
	if !board.ReviewNeedsGit(planned) {
		edges, _, e := board.ReviewClosureEdges(v, r)
		if e != nil {
			return c, e
		}
		return board.CloseReviewBatch(root, r, board.ReviewGitEvidence{CWD: cwd, Head: "N/A", NotApplicable: "review-plan: all roles explicitly N/A; no Git target", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
	}
	noDelivery := board.ReviewNoDelivery(planned)
	// A batch with a delivery closes only at its exact target on a clean worktree.
	// A batch with no delivery closes at any HEAD that still holds the target.
	verifyHead := func() (observed string, err error) {
		if noDelivery {
			return verifyNoDeliveryHead(cwd, v.Batch.TargetCommit)
		}
		return "", verifyClosureHead(cwd, v.Batch.TargetCommit)
	}
	if _, err = verifyHead(); err != nil {
		return c, err
	}
	// Before ReviewClosureEdges, which refuses a fix commit equal to the reviewed commit as one error
	// for the whole batch: the claim check lists every affected record instead.
	if err = verifyFixCommitClaims(cwd, v, r); err != nil {
		return c, err
	}
	edges, _, err := board.ReviewClosureEdges(v, r)
	if err != nil {
		return c, err
	}
	for _, edge := range edges {
		if err = verifyGitEdge(cwd, edge); err != nil {
			return c, err
		}
	}
	mechanical, err := verifyMechanicalGit(cwd, v, r)
	if err != nil {
		return c, err
	}
	// Recheck after the potentially long Git work; board then CASes the exact view.
	observed, err := verifyHead()
	if err != nil {
		return c, err
	}
	evidence := board.ReviewGitEvidence{CWD: cwd, Head: v.Batch.TargetCommit, Mechanical: mechanical, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if noDelivery {
		evidence.NoDelivery, evidence.ObservedHead = true, observed
	}
	return board.CloseReviewBatch(root, r, evidence)
}

// resolveBatchWithGit verifies the claims of a resolution the way closeWithGit does:
// board names the edges, this layer runs Git, and board compares the evidence.
func resolveBatchWithGit(root, cwd string, x board.ReviewPlanExtension) error {
	edges, err := board.ReviewBatchResolutionEdges(root, x)
	if err != nil {
		return err
	}
	for _, edge := range edges {
		if err = verifyGitEdge(cwd, edge); err != nil {
			return err
		}
	}
	return board.ResolveReviewBatch(root, x, board.ReviewResolutionGit{CWD: cwd, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
}

func hydrateIncremental(root string, options archiveOptions, arguments []string, replay bool) ([]string, error) {
	if options.previousID == "" {
		return arguments, nil
	}
	previous, err := board.ReadReviewRun(root, options.previousID)
	if err != nil {
		return nil, err
	}
	// A new run names the members that remain. A replay keeps the frozen members
	// of its own run, which the replay check has already compared.
	remaining, err := board.ReviewRemainingMembers(root, previous.TaskIDs)
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
		context, err = board.ReadReviewInput(root, options.runID, "review-context.md")
	} else {
		context, err = board.ReviewIncrementalContext(root, options.previousID)
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
func verifyPlanCWD(root, planID, cwd string) error {
	p, err := board.ReadReviewPlan(root, planID)
	if err != nil {
		return err
	}
	if !board.SameReviewCWD(p.CWD, cwd) {
		return archiveError("plan CWD mismatch")
	}
	return nil
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
