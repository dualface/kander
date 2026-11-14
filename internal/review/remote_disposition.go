package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// remoteDispositionCommands are the evidence commands available in server
// mode. Every claimed card supplies its own execution proof.
var remoteDispositionCommands = map[string]bool{"plan": true, "aggregate": true, "close": true, "progress": true, "extend-plan": true, "advance": true, "assign": true, "disposition": true, "interpret": true, "map-legacy": true, "confirm": true}

// runRemoteDisposition runs one evidence command against the server project.
// Git facts are verified here against the client's repository at cwd, exactly
// as the v1 command does; the server revalidates the board structure.
func runRemoteDisposition(cfg *config.Config, args []string) int {
	if len(args) < 3 || len(args) > 4 || (args[0] == "disposition" || args[0] == "confirm") != (len(args) == 4) {
		return dispositionFailure(newGateMsg(2, config.CommandUsage("review")))
	}
	action, cwd, input := args[0], args[1], args[2]
	if !remoteDispositionCommands[action] {
		return dispositionFailure(archiveError("review " + action + " is not available in server mode yet"))
	}
	if !filepath.IsAbs(cwd) {
		return dispositionFailure(archiveError("absolute CWD required"))
	}
	cwd = canonicalEvidenceCWD(cwd)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return dispositionFailure(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx

	var out json.RawMessage
	// typed is the board type of the answer; printing it through that type
	// gives the exact bytes the v1 command prints, which the close request
	// hashes.
	var typed any
	switch action {
	case "confirm":
		var publication api.Publication
		publication, err = session.ConfirmReviewPublication(ctx, input, args[3])
		if err == nil {
			out, err = json.Marshal(map[string]string{"publication_id": publication.ID, "task_id": args[3], "status": "confirmed", "digest": publication.Digest})
		}
		typed = &map[string]string{}

	case "plan":
		var p board.ReviewPlan
		if err = readArchiveJSON(input, &p); err == nil {
			if !filepath.IsAbs(p.CWD) || !board.SameReviewCWD(canonicalEvidenceCWD(p.CWD), cwd) {
				err = archiveError("plan CWD mismatch")
			} else {
				p.CWD = cwd
				err = validatePlanGit(p)
			}
		}
		if err == nil {
			proof, authorityErr := session.ReviewAuthority(ctx, "review-plan", p, p.TaskIDs)
			err = authorityErr
			if err == nil {
				out, err = session.Project.ReviewPlan(ctx, client.NewKey(), p, proof)
			}
		}
		typed = &map[string]string{}
	case "aggregate":
		var v board.ReviewBatchView
		if v, err = remoteBatchView(ctx, session, input); err == nil {
			proof, authorityErr := session.ReviewAuthority(ctx, "review-aggregate:"+input, input, v.Batch.TaskIDs)
			err = authorityErr
			if err == nil {
				out, err = session.Project.ReviewAggregate(ctx, client.NewKey(), input, proof)
			}
		}
		typed = &board.ReviewBatchView{}
	case "progress":
		out, err = session.Project.ReviewProgress(ctx, input)
		typed = &board.ReviewProgress{}
	case "extend-plan", "advance", "assign", "disposition", "interpret", "map-legacy":
		out, err = remoteEvidenceAction(ctx, session, cwd, action, input, args)
		typed = &map[string]string{}

	case "close":
		var r board.ReviewCloseRequest
		if err = readArchiveJSON(input, &r); err == nil {
			var v board.ReviewBatchView
			if v, err = remoteBatchView(ctx, session, r.BatchID); err == nil {
				var evidence board.ReviewGitEvidence
				if evidence, err = closureEvidence(cwd, v, r); err == nil {
					payload := map[string]any{"request": r, "evidence": evidence}
					proof, authorityErr := session.ReviewAuthority(ctx, "review-close:"+r.BatchID, payload, v.Batch.TaskIDs)
					err = authorityErr
					if err == nil {
						out, err = session.Project.ReviewClose(ctx, client.NewKey(), map[string]any{"request": r, "evidence": evidence}, proof)
					}
				}
			}
		}
		typed = &board.ReviewClosure{}
	}
	if err != nil {
		return dispositionFailure(err)
	}
	if err := json.Unmarshal(out, typed); err != nil {
		return dispositionFailure(err)
	}
	data, err := json.MarshalIndent(typed, "", "  ")
	if err != nil {
		return dispositionFailure(err)
	}
	fmt.Fprintln(os.Stdout, string(data))
	return 0
}

func remoteBatchView(ctx context.Context, session *remote.Session, batch string) (board.ReviewBatchView, error) {
	var v board.ReviewBatchView
	raw, err := session.Project.ReviewBatchView(ctx, batch)
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	return v, err
}

// closureEvidence is the Git half of closeWithGit, run on this host.
func closureEvidence(cwd string, v board.ReviewBatchView, r board.ReviewCloseRequest) (board.ReviewGitEvidence, error) {
	now := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	edges, _, err := board.ReviewClosureEdges(v, r)
	if err != nil {
		return board.ReviewGitEvidence{}, err
	}
	if !board.ReviewNeedsGit(board.ReviewPlanBatch{Base: v.Batch.Base, TargetCommit: v.Batch.TargetCommit, Requirements: v.Batch.Requirements}) {
		return board.ReviewGitEvidence{CWD: cwd, Head: "N/A", NotApplicable: "review-plan: all roles explicitly N/A; no Git target", VerifiedAt: now(), Edges: edges}, nil
	}
	if err := verifyClosureHead(cwd, v.Batch.TargetCommit); err != nil {
		return board.ReviewGitEvidence{}, err
	}
	for _, edge := range edges {
		if err := verifyGitEdge(cwd, edge); err != nil {
			return board.ReviewGitEvidence{}, err
		}
	}
	mechanical, err := verifyMechanicalGit(cwd, v, r)
	if err != nil {
		return board.ReviewGitEvidence{}, err
	}
	if err := verifyClosureHead(cwd, v.Batch.TargetCommit); err != nil {
		return board.ReviewGitEvidence{}, err
	}
	return board.ReviewGitEvidence{CWD: cwd, Head: v.Batch.TargetCommit, Mechanical: mechanical, VerifiedAt: now(), Edges: edges}, nil
}

func readRemoteResource(ctx context.Context, session *remote.Session, kind, id string, value any) error {
	raw, err := session.Project.ReviewResource(ctx, kind, id)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func remoteEvidenceAction(ctx context.Context, session *remote.Session, cwd, action, input string, args []string) (json.RawMessage, error) {
	var payload any
	var tasks []string
	runMembers := func(id string) error {
		var run board.ReviewRun
		if err := readRemoteResource(ctx, session, "runs", id, &run); err != nil {
			return err
		}
		tasks = run.TaskIDs
		return nil
	}
	switch action {
	case "extend-plan":
		var request board.ReviewPlanExtension
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		var plan board.ReviewPlan
		if err := readRemoteResource(ctx, session, "plans", request.PlanID, &plan); err != nil {
			return nil, err
		}
		tasks = plan.TaskIDs
		if request.ResolveBatch != nil {
			return remoteResolveBatch(ctx, session, cwd, request)
		}
		if request.Batch != nil && board.ReviewNeedsGit(*request.Batch) {
			if err := verifyGitEdge(cwd, board.ReviewGitEdge{Ancestor: request.Batch.Base, Descendant: request.Batch.TargetCommit}); err != nil {
				return nil, err
			}
		}
		payload = request
	case "advance":
		var request board.ReviewBatchAdvance
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		var batch board.ReviewBatch
		if err := readRemoteResource(ctx, session, "batches", request.BatchID, &batch); err != nil {
			return nil, err
		}
		if batch.PlanID == "" {
			return nil, archiveError("batch has no review plan")
		}
		if err := verifyClosureHead(cwd, request.Advance.Target); err != nil {
			return nil, err
		}
		if err := validateReviewAdvance(reviewContext{root: cwd, commit: request.Advance.Target}, request.Advance, batch.TaskIDs); err != nil {
			return nil, err
		}
		var plan board.ReviewPlan
		if err := readRemoteResource(ctx, session, "plans", batch.PlanID, &plan); err != nil {
			return nil, err
		}
		tasks, payload = plan.TaskIDs, request
	case "assign":
		var request board.ReviewAssignment
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		if err := runMembers(request.RunID); err != nil {
			return nil, err
		}
		payload = request
	case "disposition":
		var request board.ReviewDisposition
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		revision, err := strconv.ParseUint(args[3], 10, 64)
		if err != nil {
			return nil, err
		}
		tasks = []string{request.TaskID}
		payload = map[string]any{"disposition": request, "expected_revision": revision}
	case "interpret":
		var request board.ReviewInterpretation
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		if err := runMembers(request.RunID); err != nil {
			return nil, err
		}
		payload = request
	case "map-legacy":
		var request board.LegacyFindingMap
		if err := readArchiveJSON(input, &request); err != nil {
			return nil, err
		}
		if err := runMembers(request.RunID); err != nil {
			return nil, err
		}
		payload = request
	}
	proof, err := session.ReviewAuthority(ctx, "review-"+action, payload, tasks)
	if err != nil {
		return nil, err
	}
	return session.Project.ReviewAction(ctx, client.NewKey(), action, payload, proof)
}
