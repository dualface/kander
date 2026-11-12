package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/remote"
)

var ensureRemoteWorker = remote.EnsureWorker

func runRemoteCoordinator(ctx context.Context, cfg *config.Config, args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	if args[0] == "show" {
		view, err := session.Project.Group(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		if view.Checkpoint == nil {
			return fail(errors.New(config.Text("remote.coordinator_checkpoint_missing")))
		}
		if err := json.NewEncoder(os.Stdout).Encode(view.Checkpoint); err != nil {
			return fail(err)
		}
		return 0
	}
	if !filepath.IsAbs(args[1]) {
		return coordinatorUsageFail()
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return fail(err)
	}
	var checkpoint board.CoordinatorCheckpoint
	switch args[0] {
	case "claim":
		var request board.CoordinatorClaim
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		grant, err := session.AcquireCoordinator(ctx, request, request.Basis)
		if err != nil {
			return fail(err)
		}
		if err := ensureRemoteWorker(session.Home); err != nil {
			fmt.Fprintln(os.Stderr, config.Text("remote.worker_start_failed", err.Error()))
		}
		checkpoint = grant.Checkpoint
	case "reconcile":
		var request board.CoordinatorReconcile
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		localCWD := request.CWD
		if localCWD == "" {
			localCWD = cwd
		}
		request.CWD = "project:" + session.Info.ID
		facts, err := session.Project.CoordinatorFacts(ctx, request.GroupID, request)
		if err != nil {
			return fail(err)
		}
		input := api.GroupReconcile{Request: request}
		for _, fact := range facts {
			check := api.CoordinatorGitCheck{Facts: fact}
			if err := verifyCoordinatorGitFacts(ctx, localCWD, fact); err != nil {
				check.Error = err.Error()
			}
			input.Checks = append(input.Checks, check)
		}
		proof, err := session.CoordinatorAuthority(request.GroupID, nil)
		if err != nil {
			return fail(err)
		}
		checkpoint, err = session.Project.CoordinatorAction(ctx, client.NewKey(), request.GroupID, "reconcile", input, proof)
		if err != nil {
			return fail(err)
		}
		if blocked := checkpoint.BlockedMembers(); len(blocked) > 0 && len(blocked) == len(checkpoint.Members) {
			err = &board.CoordinatorBlockedError{Group: request.GroupID, Reason: checkpoint.Members[blocked[0]].Blocked.Reason}
		}
		return finishReconcile(checkpoint, err)
	case "recover":
		var request board.CoordinatorRecover
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		task, err := session.Project.Show(ctx, request.Member)
		if err != nil {
			return fail(err)
		}
		input := api.GroupRecover{Request: request, ObservedRevision: task.Revision, Observation: api.AgentObservation{Status: "stopped", Reason: "finished member recovery"}}
		if task.State == "working" || task.State == "review" {
			credential, found, err := session.Credential(task.TaskID)
			if err != nil {
				return fail(err)
			}
			if !found || credential.Lost {
				return fail(&api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.coordinator_holder_recovery_required")})
			}
			reports := liveness.ObserveAgentStatus(ctx, []liveness.AgentStatusInput{{TaskID: task.TaskID, Window: credential.Window, Session: credential.Session}}, liveness.BatchOptions{})
			if reports[0].Status != liveness.Stopped {
				return fail(errors.New(config.Text("remote.coordinator_stopped_required")))
			}
			input.Observation.Reason = reports[0].Detail
		}
		proof, err := session.CoordinatorAuthority(request.GroupID, []string{task.TaskID})
		if err != nil {
			return fail(err)
		}
		checkpoint, err = session.Project.CoordinatorAction(ctx, client.NewKey(), request.GroupID, "recover", input, proof)
		if err != nil {
			return fail(err)
		}
	case "release":
		var request api.GroupRelease
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		proof, err := session.CoordinatorAuthority(request.GroupID, nil)
		if err != nil {
			return fail(err)
		}
		value, err := session.Project.ReleaseGroup(ctx, client.NewKey(), request.GroupID, request, proof)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			return fail(err)
		}
		return 0
	default:
		return coordinatorUsageFail()
	}
	if err := json.NewEncoder(os.Stdout).Encode(checkpoint); err != nil {
		return fail(err)
	}
	return 0
}
