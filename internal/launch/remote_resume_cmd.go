package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func runRemoteResume(cfg *config.Config, agent *string, launcher, task, message, messageFile string, messageSet bool, timeout float64, options DispatchOptions) int {
	if err := ValidateTimeout(timeout, "resume"); err != nil {
		return fail(err)
	}
	text, err := ReadMessage(message, messageSet, messageFile, "resume")
	if err != nil {
		return fail(err)
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
	card, err := session.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(card.Text)); err != nil {
		return fail(err)
	}
	if agent == nil && options.Decision == "" {
		credential, found, err := session.Credential(card.TaskID)
		if err != nil {
			return fail(err)
		}
		ownedStopped := found && !credential.Lost && card.Claim != nil && card.Claim.Status == api.ClaimActive && card.Claim.ClientID == session.Client.ClientID() && card.Claim.ExecutionID == credential.ExecutionID && observeLocalExecution(ctx, credential).Status == "stopped"
		if ownedStopped {
			record, err := session.Project.ExecutionRecord(ctx, card.TaskID)
			if err != nil {
				return fail(err)
			}
			ownedStopped = !record.StartUnfinished()
		}
		if ownedStopped {
			options.Decision = "native-resume:" + client.NewKey()
		} else {
			if launcher != "" {
				return fail(launchError("remote.launcher_requires_takeover"))
			}
			request, err := NotifyRemote(card.TaskID, text, "", "", true, timeout, options)
			if err != nil {
				return fail(err)
			}
			if err := json.NewEncoder(os.Stdout).Encode(request); err != nil {
				return fail(err)
			}
			return 0
		}
	}
	if strings.TrimSpace(options.Decision) == "" {
		return fail(launchError("remote.takeover_decision_required"))
	}
	newRound := options.ID != "" || options.Kind != "" || options.Base != "" || options.EvidenceFile != ""
	bound := board.MetadataFrom(card.Text, "DISPATCH_ID")
	if bound != "" && newRound {
		dispatch, err := session.Project.Dispatch(ctx, card.TaskID, bound)
		if err != nil {
			return fail(err)
		}
		if options.ID == bound {
			if text != dispatch.Input.Message || options.Kind != "" && options.Kind != dispatch.Input.Kind || options.Base != "" && options.Base != dispatch.Input.Base {
				return fail(launchError("remote.dispatch_proposal_changed"))
			}
			newRound = false
		} else if dispatch.State != board.DispatchCompleted {
			return fail(launchError("remote.finish_dispatch_before_restart"))
		}
	}
	if card.State == "review" && board.TaskGroupFrom(card.Text) != "" && !newRound && bound == "" {
		record, err := session.Project.ExecutionRecord(ctx, card.TaskID)
		if err != nil {
			return fail(err)
		}
		if !record.StartUnfinished() {
			return fail(launchError("launch.review_dispatch_required", card.TaskID))
		}
	}
	input := remoteLaunchInput{Launcher: launcher, TaskID: card.TaskID, Message: text, Decision: options.Decision, Base: options.Base, Takeover: true, Native: agent == nil}
	if agent != nil {
		input.Agent = *agent
	}
	if newRound {
		if options.ID == "" {
			options.ID, err = board.NewDispatchID()
			if err != nil {
				return fail(err)
			}
		}
		digest := sha256.Sum256([]byte(card.TaskID + "\x00" + options.ID))
		input.PendingRequest = api.CollaborationRequestID("dispatch-proposal-" + hex.EncodeToString(digest[:]))
	}
	result, err := launchRemote(input)
	for _, warning := range result.Warnings {
		fmt.Fprint(os.Stderr, warning)
	}
	if err != nil {
		return exitLaunch(err)
	}
	if result.Superseded {
		stepAside(result.TaskID, result.Plan, result.Outcome)
		return 0
	}
	if newRound {
		request, err := NotifyRemote(card.TaskID, text, "", "", true, timeout, options)
		if err != nil {
			return exitLaunch(&UnresolvedError{TaskID: card.TaskID, Window: locationOf(result.Plan, result.Outcome), Reason: err.Error(), Command: "kander"})
		}
		if err := json.NewEncoder(os.Stdout).Encode(request); err != nil {
			return fail(err)
		}
	}
	if err := reportLaunch(t("launch.started"), board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome); err != nil {
		return exitLaunch(err)
	}
	return 0
}
