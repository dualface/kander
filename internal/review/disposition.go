package review

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
)

func dispositionCommand(name string) bool {
	switch name {
	case "plan", "extend-plan", "advance", "assign", "disposition", "map-legacy", "aggregate", "close", "progress":
		return true
	}
	return false
}
func runDispositionCommand(args []string) int {
	if len(args) < 3 {
		return dispositionFailure(newGate(2, "review.gate_usage"))
	}
	action, cwd, input := args[0], args[1], args[2]
	if !filepath.IsAbs(cwd) {
		return dispositionFailure(archiveError("absolute CWD required"))
	}
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
			if p.CWD != cwd {
				err = archiveError("plan CWD mismatch")
			} else {
				err = validatePlanGit(p)
			}
			if err == nil {
				err = board.CreateReviewPlan(root, p)
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
			if err == nil && x.Batch != nil && board.ReviewNeedsGit(*x.Batch) {
				err = verifyGitEdge(cwd, board.ReviewGitEdge{Ancestor: x.Batch.Base, Descendant: x.Batch.TargetCommit})
			}
			if err == nil {
				err = board.ExtendReviewPlan(root, x)
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
	case "map-legacy":
		var m board.LegacyFindingMap
		if err = readArchiveJSON(input, &m); err == nil {
			err = board.MapLegacyReview(root, m)
		}
		result = map[string]string{"run_id": m.RunID}
	case "aggregate":
		result, err = board.PublishReviewDisposition(root, input)
	case "progress":
		result, err = board.ReviewTaskProgress(root, input)
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
		if err := verifyGitEdge(p.CWD, board.ReviewGitEdge{Ancestor: b.Base, Descendant: b.TargetCommit}); err != nil {
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
func closeWithGit(root, cwd string, r board.ReviewCloseRequest) (board.ReviewClosure, error) {
	var c board.ReviewClosure
	v, err := board.ReadReviewBatchView(root, r.BatchID)
	if err != nil {
		return c, err
	}
	if !board.ReviewNeedsGit(board.ReviewPlanBatch{Base: v.Batch.Base, TargetCommit: v.Batch.TargetCommit, Requirements: v.Batch.Requirements}) {
		edges, _, e := board.ReviewClosureEdges(v, r)
		if e != nil {
			return c, e
		}
		return board.CloseReviewBatch(root, r, board.ReviewGitEvidence{CWD: cwd, Head: "N/A", NotApplicable: "review-plan: all roles explicitly N/A; no Git target", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
	}
	if err = verifyClosureHead(cwd, v.Batch.TargetCommit); err != nil {
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
	if err = verifyClosureHead(cwd, v.Batch.TargetCommit); err != nil {
		return c, err
	}
	return board.CloseReviewBatch(root, r, board.ReviewGitEvidence{CWD: cwd, Head: v.Batch.TargetCommit, Mechanical: mechanical, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
}

func hydrateIncremental(root string, options archiveOptions, arguments []string, replay bool) ([]string, error) {
	if options.previousID == "" {
		return arguments, nil
	}
	previous, err := board.ReadReviewRun(root, options.previousID)
	if err != nil {
		return nil, err
	}
	if previous.BatchID != options.batchID || !slices.Equal(previous.TaskIDs, options.tasks) {
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
	if p.CWD != cwd {
		return archiveError("plan CWD mismatch")
	}
	return nil
}
