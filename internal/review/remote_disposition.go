package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// remoteDispositionCommands are the evidence commands available in server
// mode. They cover the review gate of a single card (plan, aggregate, close,
// progress); the others need joint holder confirmation and are refused.
var remoteDispositionCommands = map[string]bool{"plan": true, "aggregate": true, "close": true, "progress": true}

// runRemoteDisposition runs one evidence command against the server project.
// Git facts are verified here against the client's repository at cwd, exactly
// as the v1 command does; the server revalidates the board structure.
func runRemoteDisposition(cfg *config.Config, args []string) int {
	if len(args) != 3 {
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
	claimOf := func(tasks []string) *client.Claim {
		for _, id := range tasks {
			if c, found, err := session.Credential(id); err == nil && found {
				return c.Claim()
			}
		}
		return nil
	}
	var out json.RawMessage
	// typed is the board type of the answer; printing it through that type
	// gives the exact bytes the v1 command prints, which the close request
	// hashes.
	var typed any
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
		}
		if err == nil {
			out, err = session.Project.ReviewPlan(ctx, client.NewKey(), p, claimOf(p.TaskIDs))
		}
		typed = &map[string]string{}
	case "aggregate":
		var v board.ReviewBatchView
		if v, err = remoteBatchView(ctx, session, input); err == nil {
			out, err = session.Project.ReviewAggregate(ctx, client.NewKey(), input, claimOf(v.Batch.TaskIDs))
		}
		typed = &board.ReviewBatchView{}
	case "progress":
		out, err = session.Project.ReviewProgress(ctx, input)
		typed = &board.ReviewProgress{}
	case "close":
		var r board.ReviewCloseRequest
		if err = readArchiveJSON(input, &r); err == nil {
			var v board.ReviewBatchView
			if v, err = remoteBatchView(ctx, session, r.BatchID); err == nil {
				var evidence board.ReviewGitEvidence
				if evidence, err = closureEvidence(cwd, v, r); err == nil {
					out, err = session.Project.ReviewClose(ctx, client.NewKey(), map[string]any{"request": r, "evidence": evidence}, claimOf(v.Batch.TaskIDs))
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
