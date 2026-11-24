package launch

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// The deadlines of a coordinator command in a distributed project. Tests
// replace them; nothing else writes them.
var (
	// coordinatorTimeout bounds opening the service session and the requests of
	// every coordinator subcommand except integrate and integration-show,
	// including the facts request of reconcile.
	coordinatorTimeout = 30 * time.Second
	// reconcileVerifyTimeout bounds the local Git verification of reconcile:
	// every Git command and service read between the facts request and the
	// reconcile request.
	reconcileVerifyTimeout = 5 * time.Minute
	// reconcileRequestTimeout bounds the reconcile request. It starts when the
	// local verification ends.
	reconcileRequestTimeout = 60 * time.Second
)

// Codes of the reconcile failures that are not a service answer.
const (
	codeReconcileVerifyInterrupted = "reconcile_verify_interrupted"
	codeReconcileNotSent           = "reconcile_not_sent"
	codeReconcileResultUnknown     = "reconcile_result_unknown"
)

// remoteReconcile runs reconcile through the service. requestCtx carries the
// deadline of the facts request; parent carries only the cancellation of the
// command, and each later phase takes its own deadline from it. path is the
// request file the next steps of a failure name.
func remoteReconcile(parent, requestCtx context.Context, session *remote.Session, cwd, path string, request board.CoordinatorReconcile) (board.CoordinatorCheckpoint, error) {
	localCWD := request.CWD
	if localCWD == "" {
		localCWD = cwd
	}
	request.CWD = "project:" + session.Info.ID
	facts, err := session.Project.CoordinatorFacts(requestCtx, request.GroupID, request)
	if err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	checks, err := reconcileGitChecks(parent, session, localCWD, path, facts)
	if err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	proof, err := session.CoordinatorAuthority(request.GroupID, nil)
	if err != nil {
		return board.CoordinatorCheckpoint{}, err
	}
	checkpoint, err := postReconcile(parent, session, path, api.GroupReconcile{Request: request, Checks: checks}, proof)
	if err != nil {
		return checkpoint, err
	}
	if blocked := checkpoint.BlockedMembers(); len(blocked) > 0 && len(blocked) == len(checkpoint.Members) {
		err = &board.CoordinatorBlockedError{Group: request.GroupID, Reason: checkpoint.Members[blocked[0]].Blocked.Reason}
	}
	return checkpoint, err
}

// reconcileGitChecks verifies every fact in this clone under one deadline and
// returns the verdicts. Within the deadline a fetch with the same arguments
// runs once, and equal facts are verified once. A deadline or a cancellation
// is no verdict: it fails the command, so a slow network never blocks a
// member.
func reconcileGitChecks(parent context.Context, session *remote.Session, cwd, path string, facts []board.CoordinatorGitFacts) ([]api.CoordinatorGitCheck, error) {
	ctx, cancel := context.WithTimeout(remote.WithFetchOnce(parent), reconcileVerifyTimeout)
	defer cancel()
	checks := make([]api.CoordinatorGitCheck, 0, len(facts))
	for _, fact := range facts {
		if check, found := verifiedFact(checks, fact); found {
			checks = append(checks, check)
			continue
		}
		err := verifyRemoteCoordinatorGitFacts(ctx, session, cwd, fact)
		if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil) {
			return nil, &command.Failure{Code: codeReconcileVerifyInterrupted, Message: config.Text("remote.reconcile_verify_interrupted", describeCoordinatorFact(fact), err.Error(), reconcileVerifyTimeout, path), Err: err}
		}
		checks = append(checks, coordinatorGitCheck(fact, err))
	}
	return checks, nil
}

// verifiedFact returns the verdict of an equal fact verified earlier.
func verifiedFact(checks []api.CoordinatorGitCheck, fact board.CoordinatorGitFacts) (api.CoordinatorGitCheck, bool) {
	for _, check := range checks {
		if reflect.DeepEqual(check.Facts, fact) {
			return check, true
		}
	}
	return api.CoordinatorGitCheck{}, false
}

// describeCoordinatorFact names the member and fact a verification was about.
func describeCoordinatorFact(fact board.CoordinatorGitFacts) string {
	if d := fact.Delivery; d != nil {
		return config.Text("remote.reconcile_fact_delivery", d.TaskID, d.Commit)
	}
	g := fact.Integration
	return config.Text("remote.reconcile_fact_wrap_up", g.TaskID, g.DispatchID, g.IntegrationID)
}

// postReconcile sends the reconcile request under its own deadline. A request
// the command can no longer send is not sent; a request whose deadline ended
// without an answer may have been applied, and the same request file settles
// it: the service replays a request whose hash it recorded last.
func postReconcile(parent context.Context, session *remote.Session, path string, input api.GroupReconcile, proof client.Claim) (board.CoordinatorCheckpoint, error) {
	group := input.Request.GroupID
	if err := parent.Err(); err != nil {
		return board.CoordinatorCheckpoint{}, &command.Failure{Code: codeReconcileNotSent, Message: config.Text("remote.reconcile_not_sent", group, err.Error(), path), Err: err}
	}
	ctx, cancel := context.WithTimeout(parent, reconcileRequestTimeout)
	defer cancel()
	checkpoint, err := session.Project.CoordinatorAction(ctx, client.NewKey(), group, "reconcile", input, proof)
	if err != nil && client.IsUnreachable(err) && ctx.Err() != nil {
		return checkpoint, &command.Failure{Code: codeReconcileResultUnknown, Message: config.Text("remote.reconcile_result_unknown", group, reconcileRequestTimeout, path), Err: err}
	}
	return checkpoint, err
}
