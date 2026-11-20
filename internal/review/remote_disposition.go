package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// remoteDispositionCommands are the evidence commands available in server
// mode. The holder of a claimed card proves its own execution; a group
// coordinator process without that credential asks the holder to confirm a
// joint publication instead, except for disposition, which only the holder
// submits.
var remoteDispositionCommands = map[string]bool{"plan": true, "aggregate": true, "close": true, "progress": true, "extend-plan": true, "advance": true, "assign": true, "disposition": true, "interpret": true, "map-legacy": true, "abandon": true, "confirm": true, "publication": true}

// runRemoteDisposition runs one evidence command against the server project.
// Git facts are verified here against the client's repository at cwd, exactly
// as the v1 command does; the server revalidates the board structure.
func runRemoteDisposition(cfg *config.Config, args []string) int {
	if err := remote.RequireBinding(cfg); err != nil {
		return dispositionFailure(err)
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
		confirmed, confirmErr := confirmPublication(ctx, session, input, args[3])
		if confirmErr != nil {
			return dispositionFailure(confirmErr)
		}
		return printMaterial(confirmed)
	case "publication":
		view, viewErr := readPublicationView(ctx, session, input)
		if viewErr != nil {
			return dispositionFailure(viewErr)
		}
		return printMaterial(view)

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
		out, err = session.AggregateReview(ctx, input)
		typed = &board.ReviewBatchView{}
	case "progress":
		out, err = session.Project.ReviewProgress(ctx, input)
		typed = &board.ReviewProgress{}
	case "extend-plan", "advance", "assign", "disposition", "interpret", "map-legacy", "abandon":
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
	if view, ok := typed.(*board.ReviewBatchView); ok && command.JSON() {
		return printDisposition(aggregateResult(view))
	}
	return printDisposition(typed)
}

// printDisposition prints the answer of an evidence command: the JSON object of --json, or the
// same object indented. The text of an answer that other commands hash (the aggregate view)
// escapes <, > and &; printMaterial does not, so the <U+XXXX> markers of a cleaned publication
// stay readable.
func printDisposition(result any) int { return printObject(result, true) }

func printMaterial(result any) int { return printObject(result, false) }

func printObject(result any, escapeHTML bool) int {
	if command.JSON() {
		return command.Result(result)
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(escapeHTML)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return dispositionFailure(err)
	}
	fmt.Fprint(os.Stdout, out.String())
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
	if _, err := verifyBatchHead(cwd, v.Batch); err != nil {
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
	observed, err := verifyBatchHead(cwd, v.Batch)
	if err != nil {
		return board.ReviewGitEvidence{}, err
	}
	return closureGitEvidence(cwd, v.Batch, mechanical, edges, observed), nil
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
		// add_members brings its new members into the joint confirmation set, as it
		// does into the authority set of the server.
		var err error
		if tasks, err = board.ReviewPlanExtensionTasks(plan, request); err != nil {
			return nil, err
		}
		if request.ResolveBatch != nil {
			return remoteResolveBatch(ctx, session, cwd, request)
		}
		if request.Batch != nil && board.ReviewNeedsGit(*request.Batch) {
			if err := verifyPlannedBatchGit(cwd, *request.Batch); err != nil {
				return nil, err
			}
		}
		if len(request.RetargetBatches) > 0 {
			if err := verifyRetargetGit(cwd, plan, request); err != nil {
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
	case "abandon":
		var request board.ReviewAbandonRequest
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

// aggregateResult is the aggregate view with its view_hash. The digest of the view is the one
// close compares, taken over the indented text form, so the compact JSON line cannot be hashed
// by the caller.
func aggregateResult(view *board.ReviewBatchView) any {
	data, err := json.Marshal(view)
	if err != nil {
		return view
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return view
	}
	hash, _ := json.Marshal(board.ReviewViewDigest(*view))
	fields["view_hash"] = hash
	return fields
}
