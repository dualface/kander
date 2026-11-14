package launch

import (
	"context"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

func remoteOrchestrateBoard(ctx context.Context, cfg *config.Config) (board.Board, *remote.Session, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return board.Board{}, nil, "", err
	}
	service, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return board.Board{}, nil, "", err
	}
	operationCtx, finishOperation := service.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	snapshot, err := service.Project.Snapshot(ctx, api.SnapshotQuery{})
	if err != nil {
		return board.Board{}, nil, "", err
	}
	loaded, err := board.RestoreView(snapshot.View)
	return loaded, service, filepath.Join(remote.ProjectDir(cwd), "task-context"), err
}

// bindRemoteOrchestrator preserves one fixed process identity for every group
// the session later claims. Separate task executors clear that coordinator ID.
func bindRemoteOrchestrator(plan *LaunchPlan, inv process.ProcessInvocation, session *AgentSession, service *remote.Session, program *process.AgentProgram, task, project string, journal *remote.ActionJournal) (process.ProcessInvocation, func(LaunchOutcome) error, func() (AgentSession, error), error) {
	execution := journal.Value.ExecutionID
	inv = withInvocationEnvironment(*plan, inv, map[string]string{remote.EnvExecutionID: execution, remote.EnvCoordinatorExecutionID: execution})
	value := remote.ExecutionContext{ServerURL: service.ServerURL, ClientID: service.Client.ClientID(), ProjectID: service.Info.ID, ExecutionID: execution}
	if session.Reference != "" {
		value.Session = session.Render()
	}
	if !plan.capabilities().Container {
		value.Window = plan.Launcher
	}
	save := func() error {
		if err := remote.SaveExecutionContext(service.Home, value); err != nil {
			return err
		}
		journal.Value.Session, journal.Value.Window = value.Session, value.Window
		return journal.Save()
	}
	if err := save(); err != nil {
		return inv, nil, nil, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return inv, nil, nil, err
	}
	definition := config.AgentFor(cfg, session.Agent).Session
	previous, err := sessionDiscoverSnapshot(definition, task, session.Reference == "" && config.SessionDiscoversAfterStart(definition), program, project, plan.launchTimeout)
	if err != nil {
		return inv, nil, nil, err
	}
	started := nowFn()
	plan.sessionFinalize = func(effective AgentSession) error {
		if effective.Reference != "" {
			value.Session = effective.Render()
		}
		return save()
	}
	if !plan.capabilities().Container {
		plan.processRecord = func(pid int, running func() bool) <-chan struct{} {
			done := make(chan struct{})
			defer close(done)
			facts, err := process.ObserveStarted(context.Background(), pid, running)
			if err == nil {
				value.AgentPID, value.AgentStart = facts.PID, facts.Start
				value.BootID, err = process.BootIdentity()
			}
			if err == nil {
				err = save()
			}
			if err != nil && plan.warning != nil {
				plan.warning(err.Error() + "\n")
			}
			return done
		}
	}
	location := func(outcome LaunchOutcome) error { value.Window = locationOf(*plan, outcome); return save() }
	paneSession := func() (AgentSession, error) {
		if session.Reference != "" || !config.SessionDiscoversAfterStart(definition) {
			return *session, nil
		}
		reference, err := runSessionDiscoverHook(definition, task, previous, started, program, project, plan.launchTimeout)
		return AgentSession{Agent: session.Agent, Reference: reference}, err
	}
	return inv, location, paneSession, nil
}
