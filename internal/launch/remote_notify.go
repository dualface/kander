package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminal"
)

// NotifyRemote submits a generation-bound request to the owning worker. Its
// result is a queued proposal, never an assertion of business acceptance.
func NotifyRemote(task, message, messageFile, pane string, messageSet bool, timeout float64, options DispatchOptions) (api.CollabRequest, error) {
	return notifyRemoteForExecution(task, message, messageFile, pane, messageSet, timeout, options, "")
}

func notifyRemoteForExecution(task, message, messageFile, pane string, messageSet bool, timeout float64, options DispatchOptions, expectedExecution string) (api.CollabRequest, error) {
	var out api.CollabRequest
	if err := ValidateTimeout(timeout, "notify"); err != nil {
		return out, err
	}
	if timeout > float64((24*time.Hour)/time.Second) {
		return out, launchError("remote.request_timeout_bound")
	}
	message, err := ReadMessage(message, messageSet, messageFile, "notify")
	if err != nil {
		return out, err
	}
	if !utf8.ValidString(message) {
		return out, launchError("board.task_document_is_not_valid_utf_8", messageFile)
	}
	cfg, err := loadEffective()
	if err != nil {
		return out, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return out, err
	}
	card, err := session.Project.Show(ctx, task)
	if err != nil {
		return out, err
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(card.Text)); err != nil {
		return out, err
	}
	logical := struct {
		Task, Message, Pane string
		Options             DispatchOptions
	}{Task: card.TaskID, Message: message, Pane: pane, Options: options}
	if recovered, found, err := session.RecoverRequest(ctx, logical); found || err != nil {
		return recovered, err
	}
	if expectedExecution != "" && (card.Claim == nil || card.Claim.ExecutionID != expectedExecution || card.Claim.Status != api.ClaimActive) {
		return out, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.git_owner_changed")}
	}
	if card.State != "working" && card.State != "review" {
		return out, launchError("launch.only_tasks_in_review_or_working_can_be_resumed", card.TaskID, card.State)
	}
	credential, local, err := session.Credential(card.TaskID)
	if err != nil {
		return out, err
	}
	if pane != "" {
		if !local || card.Claim == nil || card.Claim.ClientID != session.Client.ClientID() || card.Claim.ExecutionID != credential.ExecutionID {
			return out, launchError("remote.foreign_pane_override")
		}
		backend, address, ok := terminal.ParseWindow(credential.Window)
		if !ok || pane != address.Pane && pane != backend.OpaqueAddress(address) {
			return out, launchError("remote.foreign_pane_override")
		}
	}
	durable := options.ID != "" || options.Kind != "" || options.Base != "" || options.EvidenceFile != ""
	if !durable && card.State == "review" && (board.TaskGroupFrom(card.Text) != "" || board.MetadataFrom(card.Text, "DISPATCH_ID") != "") {
		return out, launchError("launch.review_dispatch_required", card.TaskID)
	}
	request := api.NewCollabRequest{Kind: api.RequestSync, Message: message}
	if durable {
		evidence, err := readDispatchEvidence(options.EvidenceFile)
		if err != nil {
			return out, err
		}
		if options.ID != "" {
			previous, err := session.Project.Requests(ctx, card.TaskID)
			if err != nil {
				return out, err
			}
			for _, candidate := range previous {
				if candidate.Dispatch == nil || candidate.Dispatch.ID != options.ID {
					continue
				}
				old := candidate.Dispatch
				if old.Message != message || options.Kind != "" && old.Kind != options.Kind || options.Base != "" && old.Base != options.Base || options.EvidenceFile != "" && !reflect.DeepEqual(old.Evidence, evidence) {
					return out, &api.Error{Code: api.CodeIdempotencyMismatch, Message: config.Text("remote.dispatch_proposal_changed")}
				}
				return candidate, nil
			}
		}
		if options.ID == "" {
			options.ID, err = board.NewDispatchID()
			if err != nil {
				return out, err
			}
		}
		if options.Kind == "" {
			options.Kind = "fix"
		}
		if options.Base == "" {
			dir := cwd
			if local && credential.Worktree != "" {
				dir = credential.Worktree
			}
			options.Base, err = remote.LocalCommit(ctx, dir)
			if err != nil {
				return out, err
			}
		}
		request.Dispatch = &board.DispatchInput{ID: options.ID, TaskID: card.TaskID, Kind: options.Kind, Base: options.Base, Message: message, Evidence: evidence}
		if err := completeRemoteDispatchInput(ctx, session, cwd, request.Dispatch); err != nil {
			return out, err
		}
		request.CardRevision, request.AcceptWithin = card.Revision, api.DurationOf(time.Duration(timeout*float64(time.Second)))
		if options.Kind == "fix" {
			request.Kind = api.RequestFix
		}
	}
	if card.Claim == nil || card.Claim.Status != api.ClaimActive {
		return out, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.git_owner_changed")}
	}
	request.TargetGeneration = card.Claim.Generation
	key := client.NewKey()
	if request.Dispatch != nil {
		digest := sha256.Sum256([]byte(card.TaskID + "\x00" + request.Dispatch.ID))
		key = "dispatch-proposal-" + hex.EncodeToString(digest[:])
	}
	return session.SendRequest(ctx, key, card.TaskID, request, logical)
}
