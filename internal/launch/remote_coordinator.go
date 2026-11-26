package launch

import (
	"context"
	"errors"
	"os"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// ensureRemoteWorker is remote.EnsureWorkerChecked; tests replace it so no
// worker process starts. Only the entrances that print a notice use it
// directly; the others go through startRemoteWorker.
var ensureRemoteWorker = remote.EnsureWorkerChecked

// startRemoteWorker makes sure a worker runs and drops the version notice, for
// the callers whose command already compared the versions or that show none.
func startRemoteWorker(home string) error {
	_, err := ensureRemoteWorker(home)
	return err
}

// runRemoteCoordinator runs a coordinator subcommand through the service.
// parent carries the cancellation of the command; the deadlines come from
// coordinatorTimeout and, for reconcile, from the deadlines of its phases.
func runRemoteCoordinator(parent context.Context, cfg *config.Config, args []string) int {
	ctx, cancel := context.WithTimeout(parent, coordinatorTimeout)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	if args[0] == "show" {
		view, err := session.Project.Group(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		if view.Checkpoint == nil {
			return fail(errors.New(config.Text("remote.coordinator_checkpoint_missing")))
		}
		if code := command.Object(view.Checkpoint); code != 0 {
			return code
		}
		return 0
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
		if notice, err := ensureRemoteWorker(session.Home); err != nil {
			command.Notify("worker_start_failed", config.Text("remote.worker_start_failed", err.Error()))
		} else if notice != "" {
			command.Notify("worker_version_warning", notice)
		}
		checkpoint = grant.Checkpoint
	case "reconcile":
		var request board.CoordinatorReconcile
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		return finishReconcile(remoteReconcile(parent, ctx, session, cwd, args[1], request))
	case "recover":
		var request board.CoordinatorRecover
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		checkpoint, err = recoverRemoteCoordinatorMember(ctx, session, request)
		if err != nil {
			return fail(err)
		}
	case "remove-member":
		var request board.CoordinatorRemoveMember
		if err := board.DecodeReviewJSON(data, &request); err != nil {
			return fail(err)
		}
		proof, err := session.CoordinatorAuthority(request.GroupID, nil)
		if err != nil {
			return fail(err)
		}
		checkpoint, err = session.Project.CoordinatorAction(ctx, client.NewKey(), request.GroupID, "remove-member", request, proof)
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
		if code := command.Object(value); code != 0 {
			return code
		}
		command.Notify("coordinator_release_final", config.Text("remote.coordinator_release_final", value.Claim.ExecutionID, request.GroupID, value.Claim.Generation))
		return 0
	}
	if code := command.Object(checkpoint); code != 0 {
		return code
	}
	return 0
}
