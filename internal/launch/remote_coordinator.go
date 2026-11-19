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
	"github.com/dualface/kander/internal/remote"
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

func runRemoteCoordinator(ctx context.Context, cfg *config.Config, args []string) int {
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
		if notice, err := ensureRemoteWorker(session.Home); err != nil {
			fmt.Fprintln(os.Stderr, config.Text("remote.worker_start_failed", err.Error()))
		} else if notice != "" {
			fmt.Fprintln(os.Stderr, notice)
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
			input.Checks = append(input.Checks, coordinatorGitCheck(fact, verifyRemoteCoordinatorGitFacts(ctx, session, localCWD, fact)))
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
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			return fail(err)
		}
		fmt.Fprintln(os.Stderr, config.Text("remote.coordinator_release_final", value.Claim.ExecutionID, request.GroupID, value.Claim.Generation))
		return 0
	default:
		return coordinatorUsageFail()
	}
	if err := json.NewEncoder(os.Stdout).Encode(checkpoint); err != nil {
		return fail(err)
	}
	return 0
}
