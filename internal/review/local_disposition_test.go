package review

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// The review tests run against a board directory: evidence commands go through
// the board library and review runs use the store of that directory. A test
// that runs a server calls useServer.
func init() {
	runDisposition = runLocalDisposition
	openReviewStore = func(_ *config.Config, cwd string, _ ...string) (reviewStore, error) {
		root, err := board.BoardRootAt(cwd)
		return localReviewStore{root: root}, err
	}
}

// useServer binds the review commands to the server project for a test that runs a server.
func useServer(t *testing.T) {
	t.Helper()
	oldDisposition, oldStore := runDisposition, openReviewStore
	t.Cleanup(func() { runDisposition, openReviewStore = oldDisposition, oldStore })
	runDisposition, openReviewStore = runRemoteDisposition, openServerReviewStore
}

// runLocalDisposition runs one evidence command against the board directory.
func runLocalDisposition(_ *config.Config, args []string) int {
	action, cwd, input := args[0], args[1], args[2]
	if action == "confirm" || action == "abandon" || action == "publication" {
		return dispositionFailure(remote.ErrNotConfigured)
	}
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
					var p board.ReviewPlan
					if p, err = board.ReadReviewPlan(root, x.PlanID); err == nil {
						err = verifyRetargetGit(cwd, p, x)
					}
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

// closeWithGit closes the batch on a board directory. The Git half is closureEvidence, the one
// the client of a server runs, so these tests cover that path.
func closeWithGit(root, cwd string, r board.ReviewCloseRequest) (board.ReviewClosure, error) {
	v, err := board.ReadReviewBatchView(root, r.BatchID)
	if err != nil {
		return board.ReviewClosure{}, err
	}
	evidence, err := closureEvidence(cwd, v, r)
	if err != nil {
		return board.ReviewClosure{}, err
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
