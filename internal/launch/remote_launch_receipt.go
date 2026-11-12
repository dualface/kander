package launch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

func recoverRemoteLaunch(ctx context.Context, service *remote.Session, journal *remote.ActionJournal) error {
	value := &journal.Value
	if value.ClaimInput == nil {
		return launchError("remote.launch_reconciliation_required", journal.Path)
	}
	var grant api.ClaimGrant
	var err error
	if value.Takeover {
		if value.PreviousProof != nil {
			grant, err = service.Project.Handoff(ctx, value.ClaimKey, value.TaskID, *value.ClaimInput, *value.PreviousProof)
		} else {
			grant, err = service.Project.Takeover(ctx, value.ClaimKey, value.TaskID, *value.ClaimInput)
		}
	} else {
		grant, err = service.Project.Claim(ctx, value.ClaimKey, value.TaskID, *value.ClaimInput)
	}
	if err != nil {
		return errors.Join(err, launchError("remote.launch_reconciliation_required", journal.Path))
	}
	current, err := service.Project.Show(ctx, value.TaskID)
	if err != nil {
		return err
	}
	if current.Claim == nil || current.Claim.ExecutionID != value.ExecutionID || current.Claim.Generation != grant.Claim.Generation || current.Claim.Status != api.ClaimActive {
		return launchError("remote.launch_reconciliation_required", journal.Path)
	}
	credential := remote.Credential{ServerURL: service.ServerURL, ClientID: service.Client.ClientID(), ProjectID: service.Info.ID, TaskID: current.TaskID, ExecutionID: value.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, Session: cmp.Or(board.MetadataFrom(current.Text, board.FieldSession), value.Session), Window: cmp.Or(board.MetadataFrom(current.Text, board.FieldWindow), value.Window), RenewedAt: time.Now().UTC()}
	if err := service.SaveCredential(credential); err != nil {
		return err
	}
	if value.Phase == "claim-pending" {
		if err := abandonUnlaunchedGeneration(ctx, service, credential, value.Takeover, journal); err != nil {
			return err
		}
		return launchError("remote.launch_no_executor_reconciled", journal.Path)
	}
	_ = ensureRemoteWorker(service.Home)
	value.Phase = "unresolved"
	if err := journal.Save(); err != nil {
		return err
	}
	return &UnresolvedError{TaskID: current.TaskID, Window: credential.Window, Reason: fmt.Sprintf("%s (%s)", t("remote.launch_reconciliation_required", journal.Path), value.ExecutionID), Command: "kander"}
}

func freezeRemoteLaunch(journal *remote.ActionJournal, task string, input api.ClaimRequest, proof *client.Claim, takeover bool, taskFile string) error {
	journal.Value.ClaimKey = client.NewKey()
	journal.Value.ClaimInput, journal.Value.PreviousProof = &input, proof
	journal.Value.TaskID, journal.Value.Takeover, journal.Value.TaskFile = task, takeover, taskFile
	if input.Start != nil {
		journal.Value.Session, journal.Value.Window = input.Start.Session, input.Start.Window
	}
	journal.Value.Phase = "claim-pending"
	return journal.Save()
}

// abandonUnlaunchedGeneration is used only after the launcher proves that no
// executor was delivered and cleanup succeeded. Takeovers never restore an old
// card snapshot; they audit-release the failed new generation instead.
func abandonUnlaunchedGeneration(ctx context.Context, service *remote.Session, credential remote.Credential, takeover bool, journal *remote.ActionJournal) error {
	var err error
	if takeover {
		state, readErr := service.Project.ExecutionState(ctx, credential.TaskID, *credential.Claim())
		if readErr != nil {
			return readErr
		}
		presence := api.AgentObservation{Status: "stopped"}
		_, err = service.Project.Execution(ctx, client.NewKey(), credential.TaskID, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Presence: &presence, Decision: "launcher proved no executor delivery: " + journal.Value.ExecutionID}, *credential.Claim())
	} else {
		_, err = service.Project.Execution(ctx, client.NewKey(), credential.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, *credential.Claim())
	}
	if err != nil {
		return err
	}
	if err := remote.RemoveCredential(service.Home, credential); err != nil {
		return err
	}
	journal.Value.Phase = "failed"
	return journal.Save()
}

// journalAgentSession reserves session allocation too: create-chat and custom
// allocation commands are external effects before the terminal launch itself.
func journalAgentSession(journal *remote.ActionJournal, agent string, program *process.AgentProgram, cfg *config.Config) (AgentSession, error) {
	if journal == nil {
		return newAgentSession(agent, program, cfg)
	}
	if journal.Value.Phase == "session-ready" {
		name, reference, _ := strings.Cut(journal.Value.Session, " ")
		if name != agent || reference != "" && !sessionReferenceRe.MatchString(reference) {
			return AgentSession{}, launchError("remote.launch_reconciliation_required", journal.Path)
		}
		return AgentSession{Agent: name, Reference: reference}, nil
	}
	journal.Value.Phase = "allocating-session"
	if err := journal.Save(); err != nil {
		return AgentSession{}, err
	}
	session, err := newAgentSession(agent, program, cfg)
	if err != nil {
		return session, err
	}
	journal.Value.Session, journal.Value.Phase = session.Render(), "session-ready"
	return session, journal.Save()
}
