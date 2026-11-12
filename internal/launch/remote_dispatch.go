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

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func runRemoteDispatch(args []string, cfg *config.Config) int {
	if len(args) == 0 {
		return usageFail("dispatch", "")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	if args[0] == "show" {
		if len(args) != 3 {
			return usageFail("dispatch", "")
		}
		value, err := session.Project.Dispatch(ctx, args[1], args[2])
		if err != nil {
			return fail(err)
		}
		if err := PrintDispatchResult(value); err != nil {
			return fail(err)
		}
		return 0
	}
	var task string
	var command api.DispatchCommand
	action := args[0]
	switch action {
	case "prepare":
		if len(args) != 2 {
			return usageFail("dispatch", "")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return fail(err)
		}
		var input board.DispatchInput
		if err := board.DecodeReviewJSON(data, &input); err != nil {
			return fail(err)
		}
		task = input.TaskID
		if input.ID == "" {
			input.ID, err = board.NewDispatchID()
			if err != nil {
				return fail(err)
			}
		}
		command.Input = &input
	case "fail", "cancel":
		decision := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--decision" && i+1 < len(args) {
				decision = args[i+1]
				args = append(append([]string{}, args[:i]...), args[i+2:]...)
				break
			}
		}
		if len(args) != 5 {
			return usageFail("dispatch", "")
		}
		task, command.DispatchID, command.Reason, command.Decision = args[1], args[2], args[4], decision
		command.Revision, err = strconv.ParseUint(args[3], 10, 64)
		if err != nil {
			return fail(err)
		}
		command.State = board.DispatchFailed
		if action == "cancel" {
			command.State = board.DispatchCancelled
		}
		action = "end"
	case "settle":
		if len(args) != 4 {
			return usageFail("dispatch", "")
		}
		task, command.DispatchID = args[1], args[2]
		command.Revision, err = strconv.ParseUint(args[3], 10, 64)
		if err != nil {
			return fail(err)
		}
	case "authorize-wrap-up":
		if len(args) != 2 {
			return usageFail("dispatch", "")
		}
		data, err := os.ReadFile(args[1])
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
		if err := PrintDispatchResult(value); err != nil {
			return fail(err)
		}
		return 0
	default:
		return usageFail("dispatch", "")
	}
	current, err := session.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	command.CardRevision = current.Revision
	if action == "prepare" {
		if err := completeRemoteDispatchInput(ctx, session, cwd, command.Input); err != nil {
			return fail(err)
		}
	}
	credential, found, err := session.Credential(current.TaskID)
	if err != nil {
		return fail(err)
	}
	if !found || current.Claim == nil || current.Claim.ClientID != session.Client.ClientID() || current.Claim.ExecutionID != credential.ExecutionID {
		if command.Input == nil || current.Claim == nil {
			return fail(&api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.dispatch_holder_required")})
		}
		previous, err := session.Project.Requests(ctx, current.TaskID)
		if err != nil {
			return fail(err)
		}
		for _, candidate := range previous {
			if candidate.Dispatch == nil || candidate.Dispatch.ID != command.Input.ID {
				continue
			}
			stored := *candidate.Dispatch
			if candidate.AcceptWithin != 0 && command.Input.CreatedAt.IsZero() && command.Input.ConfirmBy.IsZero() {
				stored.CreatedAt, stored.ConfirmBy = time.Time{}, time.Time{}
			}
			left, _ := json.Marshal(stored)
			right, _ := json.Marshal(command.Input)
			if !bytes.Equal(left, right) {
				return fail(&api.Error{Code: api.CodeIdempotencyMismatch, Message: config.Text("remote.dispatch_proposal_changed")})
			}
			if err := json.NewEncoder(os.Stdout).Encode(candidate); err != nil {
				return fail(err)
			}
			return 0
		}
		kind := api.RequestSync
		if command.Input.Kind == "fix" {
			kind = api.RequestFix
		}
		request, err := session.Project.CreateRequest(ctx, client.NewKey(), current.TaskID, api.NewCollabRequest{Kind: kind, Message: command.Input.Message, Dispatch: command.Input, CardRevision: current.Revision, TargetGeneration: current.Claim.Generation})
		if err != nil {
			return fail(err)
		}
		// This is explicitly a proposal, never a fabricated prepared dispatch.
		if err := json.NewEncoder(os.Stdout).Encode(request); err != nil {
			return fail(err)
		}
		return 0
	}
	value, err := session.Project.DispatchAction(ctx, client.NewKey(), current.TaskID, action, command, *credential.Claim())
	if err != nil {
		return fail(err)
	}
	if err := PrintDispatchResult(value); err != nil {
		return fail(err)
	}
	return 0
}

func completeRemoteDispatchInput(ctx context.Context, session *remote.Session, cwd string, input *board.DispatchInput) error {
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
	local := w.Git
	local.CWD = cwd
	if err := verifyDispatchIntegration(ctx, local); err != nil {
		return err
	}
	w.Git.CWD = "project:" + session.Info.ID
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
		if err := completeRemoteDispatchInput(ctx, session, cwd, input.Intent); err != nil {
			return value, err
		}
		current, err := session.Project.Show(ctx, input.TaskID)
		if err != nil {
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
		local := w.Git
		local.CWD = cwd
		if err := verifyDispatchIntegration(ctx, local); err != nil {
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
