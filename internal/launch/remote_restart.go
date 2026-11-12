package launch

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

type remoteLaunchInput struct {
	Agent, Launcher, TaskID, Message, Decision, Base, PendingRequest string
	Takeover, Native                                                 bool
}

type remoteRestart struct {
	Continuation    *remote.Continuation
	Subject         *remote.Credential
	Request         api.ClaimRequest
	Proof           *client.Claim
	Session         AgentSession
	Dispatch        *board.Dispatch
	Base, SourceDir string
}

func ResumeRemote(agent, launcher, task, message, decision, base string, native bool) (StartResult, error) {
	return launchRemote(remoteLaunchInput{Agent: agent, Launcher: launcher, TaskID: task, Message: message, Decision: decision, Base: base, Takeover: true, Native: native})
}

func prepareRemoteRestart(ctx context.Context, session *remote.Session, task api.Task, cwd string, input remoteLaunchInput) (out remoteRestart, err error) {
	if task.State != "working" && task.State != "review" {
		return out, launchError("launch.only_tasks_in_review_or_working_can_be_resumed", task.TaskID, task.State)
	}
	if strings.TrimSpace(input.Decision) == "" {
		return out, launchError("remote.takeover_decision_required")
	}
	out.Request.Decision, out.Request.ExpectedRevision = input.Decision, task.Revision
	if task.Claim != nil {
		out.Request.ExpectedGeneration = task.Claim.Generation
	}
	credential, local, err := session.Credential(task.TaskID)
	if err != nil {
		return out, err
	}
	if local {
		copy := credential
		out.Subject = &copy
	}
	local = local && task.Claim != nil && task.Claim.ClientID == session.Client.ClientID() && credential.ExecutionID == task.Claim.ExecutionID
	if input.Native && !local {
		return out, launchError("remote.native_requires_local_execution")
	}
	if task.Claim != nil && task.Claim.Status == api.ClaimActive {
		if !local || credential.Lost {
			return out, &api.Error{Code: api.CodeLeaseActive, Message: t("remote.execution_holder_required")}
		}
		state, err := session.Project.ExecutionState(ctx, task.TaskID, *credential.Claim())
		if err != nil {
			return out, err
		}
		presence := observeLocalExecution(ctx, credential)
		if presence.Status != "stopped" {
			return out, launchError("remote.restart_needs_stopped")
		}
		out.Request.ObservationTicket, out.Request.PreviousPresence, out.Proof = state.ObservationTicket, &presence, credential.Claim()
	}
	if input.Native {
		out.Session, err = sessionFrom(task.Text)
		if err != nil {
			return out, err
		}
		if credential.Session != out.Session.Render() || credential.Window != board.MetadataFrom(task.Text, board.FieldWindow) {
			return out, launchError("launch.start_identity_changed")
		}
		if observeLocalExecution(ctx, credential).Status != "stopped" {
			return out, launchError("remote.restart_needs_stopped")
		}
	}
	if id := board.MetadataFrom(task.Text, "DISPATCH_ID"); id != "" {
		value, err := session.Project.Dispatch(ctx, task.TaskID, id)
		if err != nil {
			return out, err
		}
		epoch, err := strconv.ParseUint(board.MetadataFrom(task.Text, "EXECUTION_EPOCH"), 10, 64)
		if err != nil || epoch != value.Authorization.Epoch {
			return out, launchError("board.dispatch_conflict", id)
		}
		mode := "retain"
		if value.State == board.DispatchCompleted {
			mode = "settle-completed"
		}
		out.Dispatch = &value
		out.Request.Dispatch = &board.DispatchTakeover{Authorization: value.Authorization, Revision: value.Revision, Mode: mode}
	}
	out.Base, out.SourceDir = input.Base, cwd
	if input.Native && credential.Worktree != "" {
		out.SourceDir = credential.Worktree
	}
	if session.Info.Remote != "" && input.Base != "" {
		if !api.CommitSHA(input.Base) {
			return out, launchError("remote.git_evidence_sha")
		}
		if err := session.FetchCommit(ctx, out.SourceDir, input.Base); err != nil {
			return out, err
		}
	}
	if session.Info.Remote != "" && out.Base == "" {
		if input.Native && credential.Worktree != "" {
			out.SourceDir = credential.Worktree
			out.Base, err = remote.LocalCommit(ctx, credential.Worktree)
			if err != nil {
				return out, err
			}
		} else if task.Delivery != nil {
			if _, err := session.FetchDelivery(ctx, cwd, task.TaskID, task.Delivery.Commit); err != nil {
				return out, err
			}
			out.Base = task.Delivery.Commit
		} else {
			return out, launchError("remote.takeover_source_missing")
		}
	}
	if input.Native && credential.Worktree != "" {
		value, captureErr := session.CaptureContinuation(ctx, credential, out.Base)
		if captureErr != nil {
			return out, captureErr
		}
		out.Continuation = &value
	}
	if out.Base != "" && !api.CommitSHA(out.Base) {
		return out, launchError("remote.git_evidence_sha")
	}
	return out, nil
}
