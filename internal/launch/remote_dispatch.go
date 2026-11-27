package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// dispatchArguments are the arguments of one kander dispatch subcommand.
type dispatchArguments struct {
	action   string
	args     []string
	decision string
}

func runRemoteDispatch(a dispatchArguments, cfg *config.Config) int {
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var input board.DispatchInput
	if a.action == "prepare" {
		data, err := os.ReadFile(a.args[0])
		if err != nil {
			return fail(err)
		}
		if err := board.DecodeReviewJSON(data, &input); err != nil {
			return fail(err)
		}
		if input.Kind == "check-integration" {
			return runIntegrationPreflight(ctx, cfg, a.args[0], input)
		}
	}
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	args := a.args
	if a.action == "show" {
		value, err := session.Project.Dispatch(ctx, args[0], args[1])
		if err != nil {
			return fail(err)
		}
		if code := command.Object(value); code != 0 {
			return code
		}
		return 0
	}
	var task string
	var act api.DispatchCommand
	action := a.action
	switch action {
	case "prepare":
		task = input.TaskID
		if input.ID == "" {
			input.ID, err = board.NewDispatchID()
			if err != nil {
				return fail(err)
			}
		}
		act.Input = &input
	case "fail", "cancel":
		task, act.DispatchID, act.Reason, act.Decision = args[0], args[1], args[3], a.decision
		act.Revision, err = strconv.ParseUint(args[2], 10, 64)
		if err != nil {
			return fail(err)
		}
		act.State = board.DispatchFailed
		if action == "cancel" {
			act.State = board.DispatchCancelled
		}
		action = "end"
	case "settle":
		task, act.DispatchID = args[0], args[1]
		act.Revision, err = strconv.ParseUint(args[2], 10, 64)
		if err != nil {
			return fail(err)
		}
	case "authorize-wrap-up":
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fail(err)
		}
		var input WrapUpRequest
		if err := board.DecodeReviewJSON(data, &input); err != nil {
			return fail(err)
		}
		value, err := authorizeRemoteWrapUp(ctx, session, cwd, input)
		if err != nil {
			return fail(err)
		}
		if code := command.Object(value); code != 0 {
			return code
		}
		return 0
	}
	current, err := session.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	act.CardRevision = current.Revision
	if action == "prepare" {
		if err := completeRemoteDispatchInput(ctx, session, cwd, current, act.Input); err != nil {
			return fail(err)
		}
	}
	credential, found, err := session.Credential(current.TaskID)
	if err != nil {
		return fail(err)
	}
	if !found || current.Claim == nil || current.Claim.ClientID != session.Client.ClientID() || current.Claim.ExecutionID != credential.ExecutionID {
		if act.Input == nil || current.Claim == nil {
			return fail(&api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.dispatch_holder_required")})
		}
		previous, err := session.Project.Requests(ctx, current.TaskID)
		if err != nil {
			return fail(err)
		}
		for _, candidate := range previous {
			if candidate.Dispatch == nil || candidate.Dispatch.ID != act.Input.ID {
				continue
			}
			stored := *candidate.Dispatch
			if candidate.AcceptWithin != 0 && act.Input.CreatedAt.IsZero() && act.Input.ConfirmBy.IsZero() {
				stored.CreatedAt, stored.ConfirmBy = time.Time{}, time.Time{}
			}
			left, _ := json.Marshal(stored)
			right, _ := json.Marshal(act.Input)
			if !bytes.Equal(left, right) {
				return fail(&api.Error{Code: api.CodeIdempotencyMismatch, Message: config.Text("remote.dispatch_proposal_changed")})
			}
			if code := command.Object(candidate); code != 0 {
				return code
			}
			return 0
		}
		if err := remote.CheckRequestMessage(act.Input.Message); err != nil {
			return fail(err)
		}
		kind, err := api.DispatchRequestKind(act.Input.Kind)
		if err != nil {
			return fail(err)
		}
		request, err := session.Project.CreateRequest(ctx, client.NewKey(), current.TaskID, api.NewCollabRequest{Kind: kind, Message: act.Input.Message, Dispatch: act.Input, CardRevision: current.Revision, TargetGeneration: current.Claim.Generation})
		if err != nil {
			return fail(remote.RequestCreateError(kind, err))
		}
		// This is explicitly a proposal, never a fabricated prepared dispatch.
		if code := command.Object(request); code != 0 {
			return code
		}
		return 0
	}
	value, err := session.Project.DispatchAction(ctx, client.NewKey(), current.TaskID, action, act, *credential.Claim())
	if err != nil {
		return fail(err)
	}
	if code := command.Object(value); code != 0 {
		return code
	}
	return 0
}

func completeRemoteDispatchInput(ctx context.Context, session *remote.Session, cwd string, card api.Task, input *board.DispatchInput) error {
	if session.Info.Remote != "" {
		if err := session.FetchCommit(ctx, cwd, input.Base); err != nil {
			return err
		}
	}
	w := input.Evidence.WrapUp
	if w == nil {
		return nil
	}
	copy := *w
	input.Evidence.WrapUp = &copy
	w = &copy
	if w.Git.DispatchID == "" {
		w.Git.DispatchID = input.ID
	}
	if w.Git.TaskID == "" {
		w.Git.TaskID = input.TaskID
	}
	if w.Artifact == (board.ArtifactReference{}) {
		w.Artifact = board.ArtifactReference{TaskID: input.TaskID, Path: "dispatches/" + input.ID + "/integration.json"}
	}
	previous, err := session.Project.Dispatch(ctx, input.TaskID, input.ID)
	if err == nil && previous.Input.Evidence.WrapUp != nil {
		old := previous.Input.Evidence.WrapUp.Git
		if w.Git.ReviewBase == "" {
			w.Git.ReviewBase = old.ReviewBase
		}
		if w.Git.ReviewTarget == "" {
			w.Git.ReviewTarget = old.ReviewTarget
		}
		if w.Git.VerifiedAt.IsZero() {
			w.Git.VerifiedAt = old.VerifiedAt
		}
		return nil
	}
	if err != nil && !api.IsCode(err, api.CodeNotFound) {
		return err
	}
	proposals, err := session.Project.Requests(ctx, input.TaskID)
	if err != nil {
		return err
	}
	for _, proposal := range proposals {
		if proposal.Dispatch == nil || proposal.Dispatch.ID != input.ID || proposal.Dispatch.Evidence.WrapUp == nil {
			continue
		}
		old := proposal.Dispatch.Evidence.WrapUp.Git
		if w.Git.ReviewBase == "" {
			w.Git.ReviewBase = old.ReviewBase
		}
		if w.Git.ReviewTarget == "" {
			w.Git.ReviewTarget = old.ReviewTarget
		}
		if w.Git.VerifiedAt.IsZero() {
			w.Git.VerifiedAt = old.VerifiedAt
		}
	}
	edge, err := session.Project.DispatchRange(ctx, input.TaskID)
	if err != nil {
		return err
	}
	if w.Git.ReviewBase == "" {
		w.Git.ReviewBase = edge.Ancestor
	}
	if w.Git.ReviewTarget == "" {
		w.Git.ReviewTarget = edge.Descendant
	}
	// Only local verification consumes a machine path. New shared evidence
	// stores the stable project identity instead.
	w.Git.CWD = "project:" + session.Info.ID
	if err := session.VerifyWrapUpIntegration(ctx, cwd, w.Git); err != nil {
		return err
	}
	target, err := board.IntegrationTargetOf(card.Text)
	if err != nil {
		return err
	}
	var snapshot *api.BranchSnapshot
	if portableTargetNeedsMainline(target) {
		if snapshot, err = branchSnapshotOf(ctx, session, card); err != nil {
			return err
		}
	}
	if err := verifyPortableWrapUpTarget(cwd, target, w.Git.TargetRef, snapshot); err != nil {
		return err
	}
	if w.Git.VerifiedAt.IsZero() {
		w.Git.VerifiedAt = time.Now().UTC()
	}
	return nil
}

func authorizeRemoteWrapUp(ctx context.Context, session *remote.Session, cwd string, input WrapUpRequest) (board.Dispatch, error) {
	var value board.Dispatch
	if strings.TrimSpace(input.Author) == "" || strings.TrimSpace(input.Reason) == "" {
		return value, errors.New(config.Text("remote.wrap_up_author_required"))
	}
	credential, found, err := session.Credential(input.TaskID)
	if err != nil {
		return value, err
	}
	if !found || credential.Lost {
		return value, &api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.wrap_up_holder_required")}
	}
	if input.Intent != nil {
		if input.Intent.Kind != "wrap-up" || input.Intent.TaskID != input.TaskID || input.Intent.ID != input.DispatchID {
			return value, errors.New(config.Text("remote.wrap_up_identity_mismatch"))
		}
		current, err := session.Project.Show(ctx, input.TaskID)
		if err != nil {
			return value, err
		}
		if err := completeRemoteDispatchInput(ctx, session, cwd, current, input.Intent); err != nil {
			return value, err
		}
		value, err = session.Project.DispatchAction(ctx, client.NewKey(), input.TaskID, "prepare", api.DispatchCommand{Input: input.Intent, CardRevision: current.Revision}, *credential.Claim())
		if err != nil {
			return value, err
		}
		if input.ExpectedRevision == 0 {
			input.ExpectedRevision = value.Revision
		}
	}
	value, err = session.Project.Dispatch(ctx, input.TaskID, input.DispatchID)
	if err != nil {
		return value, err
	}
	if value.State == board.DispatchCompleted {
		return value, nil
	}
	if value.WrapUpAuthority != nil {
		if value.WrapUpAuthority.Author == input.Author && value.WrapUpAuthority.Reason == input.Reason && value.WrapUpAuthority.Exit.Decision == input.ReclaimDecision {
			return value, nil
		}
		return value, errors.New(config.Text("remote.wrap_up_decision_conflict"))
	}
	if err := session.Project.ValidateDispatch(ctx, input.TaskID, input.DispatchID); err != nil {
		return value, err
	}
	current, err := session.Project.Show(ctx, input.TaskID)
	if err != nil {
		return value, err
	}
	if current.Claim == nil || current.Claim.ClientID != session.Client.ClientID() || current.Claim.ExecutionID != credential.ExecutionID {
		return value, errors.New(config.Text("remote.wrap_up_foreign_terminal"))
	}
	if w := value.Input.Evidence.WrapUp; w != nil {
		target, err := board.IntegrationTargetOf(current.Text)
		if err != nil {
			return value, err
		}
		var snapshot *api.BranchSnapshot
		if portableTargetNeedsMainline(target) {
			if snapshot, err = branchSnapshotOf(ctx, session, current); err != nil {
				return value, err
			}
		}
		if err := verifyPortableWrapUpTarget(cwd, target, w.Git.TargetRef, snapshot); err != nil {
			return value, err
		}
		if err := session.VerifyWrapUpIntegration(ctx, cwd, w.Git); err != nil {
			return value, err
		}
	}
	snapshot := board.Snapshot{Entry: board.Entry{TaskID: current.TaskID, State: current.State}, Text: current.Text, Revision: current.Revision}
	exit, err := observedDispatchExit(ctx, snapshot, input.DispatchID)
	if err != nil {
		return value, err
	}
	if input.ReclaimDecision != "" {
		exit.Outcome, exit.Decision = "reclaimed", input.ReclaimDecision
	}
	return session.Project.DispatchAction(ctx, client.NewKey(), input.TaskID, "authorize-wrap-up", api.DispatchCommand{DispatchID: input.DispatchID, Revision: input.ExpectedRevision, CardRevision: current.Revision, Author: input.Author, Reason: input.Reason, Exit: &exit}, *credential.Claim())
}
