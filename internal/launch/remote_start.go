package launch

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

// remoteCall bounds one server call of a start.
const remoteCall = 30 * time.Second

// StartRemote is kander start in server mode. The server claims the todo card
// for a new execution in one step with the start metadata; this host launches
// the agent with that execution's identity, reports the window and session
// back, and confirms the start. A launch failure rolls the claim back so the
// card returns to todo. The agent's own kander calls carry
// KANDER_EXECUTION_ID, so they present only this execution's credential.
func StartRemote(agentOverride, launcherOverride, taskID string) (result StartResult, err error) {
	if taskID == "" {
		return result, launchError("launch.task_id_is_required")
	}
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return result, err
	}
	project := remote.ProjectDir(cwd)
	ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
	session, err := remote.Open(ctx, cfg, cwd)
	cancel()
	if err != nil {
		return result, err
	}
	call := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), remoteCall)
	}
	ctx, cancel = call()
	task, err := session.Project.Show(ctx, taskID)
	cancel()
	if err != nil {
		return result, err
	}
	if task.State != "todo" {
		return result, launchError("launch.start_only_accepts_tasks_in_todo_is_in", task.TaskID, task.State)
	}
	original := task.Text
	kind := board.MetadataFrom(original, "SIZE")
	if kind != "large" {
		kind = "small"
	}
	if problem := cfg.Rules.CheckTaskGroup(taskGroupFrom(original)); problem != nil {
		return result, problem
	}
	choice, err := resolveExecution(cfg, original, kind, agentOverride, execStart)
	if err != nil {
		return result, err
	}
	agentName := choice.Agent
	if !config.HasAgent(cfg, agentName) {
		return result, launchError("launch.unsupported_agent", agentName)
	}
	launcher := launcherOverride
	if launcher == "" {
		launcher = cfg.Launcher
	}
	plan, err := prepareLaunch(launcher, project, "start")
	if err != nil {
		return result, err
	}
	if err := applyAgentDelivery(&plan, cfg, agentName); err != nil {
		return result, err
	}
	plan.warning = func(message string) { result.Warnings = append(result.Warnings, message) }
	program, err := requireAgentProgram(agentName, cfg)
	if err != nil {
		return result, err
	}
	agentSession, err := newAgentSession(agentName, program, cfg)
	if err != nil {
		return result, err
	}
	sessionDef := config.AgentFor(cfg, agentName).Session
	initialSession := agentSession.Render()
	persistSession := config.SessionPersistsAfterStart(sessionDef) || (plan.capabilities().SessionReport && config.SessionDiscoversAfterStart(sessionDef))
	discoverSession := plan.capabilities().PaneMetadata || persistSession
	previous, err := sessionDiscoverSnapshot(sessionDef, task.TaskID, discoverSession, program, project, plan.launchTimeout)
	if err != nil {
		return result, err
	}
	window := ""
	if !plan.capabilities().Container {
		window = plan.Launcher
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return result, err
	}
	body, err := startAgentPrompt(task.TaskID, paths, taskGroupFrom(original), original)
	if err != nil {
		return result, err
	}
	taskFile, err := createTaskFile(body, "kander-"+task.TaskID+"-start-", taskFileTrailer(paths))
	if err != nil {
		return result, err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	language, err := resolvePromptLanguage(original, paths)
	if err != nil {
		return result, err
	}
	prompt, err := headedTaskFileInstruction(paths, "start", task.TaskID, "", 0, language, taskFile)
	if err != nil {
		return result, err
	}
	args, err := execArguments(choice, agentSession, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	execution := remote.NewExecutionID()
	inv = withExecutionIdentity(plan, inv, execution)

	ctx, cancel = call()
	grant, err := session.Project.Claim(ctx, client.NewKey(), task.TaskID, api.ClaimRequest{
		ClientID: session.Client.ClientID(), ExecutionID: execution, Owner: agentName,
		Start: &api.StartInput{Session: initialSession, Window: window, ExecResolved: choice.record},
	})
	cancel()
	if err != nil {
		return result, err
	}
	credential := remote.Credential{
		ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: grant.Task.TaskID,
		ExecutionID: execution, Generation: grant.Claim.Generation, Token: grant.Token,
		RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: time.Now().UTC(),
	}
	if err := session.SaveCredential(credential); err != nil {
		return result, err
	}
	if err := ensureRemoteWorker(session.Home); err != nil {
		result.Warnings = append(result.Warnings, t("remote.worker_start_failed", err.Error())+"\n")
	}
	own := *credential.Claim()
	promptAttempted := false
	plan.beforePrompt = func() error { promptAttempted = true; return nil }
	plan.judge = func() launchVerdict {
		ctx, cancel := call()
		defer cancel()
		state, err := session.Project.ExecutionState(ctx, task.TaskID, own)
		if api.IsCode(err, api.CodeHeldByOther) || api.IsCode(err, api.CodeLeaseInvalid) {
			return verdictSuperseded
		}
		if err != nil {
			return verdictKeep
		}
		if state.Standing.Superseded {
			return verdictSuperseded
		}
		if state.Standing.Evidence || promptAttempted {
			return verdictKeep
		}
		return verdictCleanup
	}

	executionUpdate := func(u api.ExecutionUpdate) error {
		ctx, cancel := call()
		defer cancel()
		_, err := session.Project.Execution(ctx, client.NewKey(), task.TaskID, u, own)
		return err
	}

	if !plan.capabilities().Container {
		plan.processRecord = func(pid int, running func() bool) <-chan struct{} {
			done := make(chan struct{})
			defer close(done)
			facts, err := process.ObserveStarted(context.Background(), pid, running)
			if err == nil {
				promptAttempted = true
				credential.AgentPID, credential.AgentStart = facts.PID, facts.Start
				credential.BootID, err = process.BootIdentity()
			}
			if err == nil {
				err = remote.WriteCredential(session.Home, credential)
			}
			if err != nil {
				result.Warnings = append(result.Warnings, err.Error()+"\n")
			}
			return done
		}
	}
	var launchStart time.Time
	var paneCB func() (AgentSession, error)
	if discoverSession {
		paneCB = func() (AgentSession, error) {
			if agentSession.Reference != "" || !config.SessionDiscoversAfterStart(sessionDef) {
				return agentSession, nil
			}
			ref, err := runSessionDiscoverHook(sessionDef, task.TaskID, previous, launchStart, program, project, plan.launchTimeout)
			if err != nil {
				return AgentSession{}, err
			}
			return AgentSession{Agent: agentSession.Agent, Reference: ref}, nil
		}
	}
	if persistSession {
		plan.sessionFinalize = func(effective AgentSession) error {
			credential.Session = effective.Render()
			if err := remote.WriteCredential(session.Home, credential); err != nil {
				return err
			}
			_ = ensureRemoteWorker(session.Home)
			return executionUpdate(api.ExecutionUpdate{Action: api.ExecutionConfirm, SessionBefore: initialSession, SessionAfter: effective.Render()})
		}
	}
	var location func(LaunchOutcome) error
	if plan.capabilities().Container {
		location = func(outcome LaunchOutcome) error {
			credential.Window = locationOf(plan, outcome)
			if err := remote.WriteCredential(session.Home, credential); err != nil {
				return err
			}
			return executionUpdate(api.ExecutionUpdate{Action: api.ExecutionWindow, Window: credential.Window})
		}
	}
	name := windowName(board.Entry{TaskID: task.TaskID}, original)
	launchStart = nowFn()
	// launchAgent starts the agent in the parent of root; the server-mode
	// root is only that anchor, never a board.
	outcome, err := launchAgent(plan, filepath.Join(project, "kanban"), name, inv, location, paneCB, &agentSession, true)
	result.TaskID, result.Size, result.Agent = task.TaskID, kind, agentName
	result.Plan, result.Outcome = plan, outcome
	if err != nil {
		failure := asLaunchFailure(err)
		result.Outcome = failure.Outcome
		if failure.Verdict == verdictSuperseded {
			result.Superseded = true
			taskFileHandedOff = true
			return result, nil
		}
		if !failure.DeliveryUnknown && failure.Verdict == verdictCleanup && failure.CloseError == "" {
			if rollbackErr := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionRollback}); rollbackErr == nil {
				_ = remote.RemoveCredential(session.Home, credential)
				return result, failure.Err
			} else {
				err = errors.Join(failure.Err, rollbackErr)
			}
		}
		taskFileHandedOff = true
		if failure.CloseError != "" {
			err = errors.Join(failure.Err, errors.New(failure.CloseError))
		}
		reason := failure.Err.Error()
		if err != nil {
			reason = err.Error()
		}
		reason = issue.Sanitize(reason)
		window := credential.Window
		if failure.Outcome.Container != "" {
			window = locationOf(plan, failure.Outcome)
		}
		if recordErr := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionUnresolve, Window: window, Reason: reason}); recordErr != nil {
			result.Warnings = append(result.Warnings, recordErr.Error()+"\n")
		}
		unresolved := &UnresolvedError{TaskID: task.TaskID, Window: window, Reason: reason, Command: "kander"}
		unresolved.Unmarked = plan.capabilities().PaneMetadata && failure.Outcome.Container != "" && !failure.Outcome.SessionMarked
		unresolved.Repairable = unresolved.Unmarked && markerRepairable(agentSession.Render())
		if plan.OccupiesTerminal() && failure.Outcome.Wait != nil {
			unresolved.hold = func() { _, _ = failure.Outcome.Wait() }
		}
		return result, unresolved
	}
	taskFileHandedOff = true
	if outcome.Process != nil {
		credential.AgentPID = outcome.Process.Pid
		if facts, observeErr := process.ObserveProcess(context.Background(), credential.AgentPID); observeErr == nil {
			credential.AgentStart = facts.Start
			credential.BootID, _ = process.BootIdentity()
		}
		credential.Session = agentSession.Render()
		if credential.Window == "" {
			credential.Window = window
		}
		if err := remote.WriteCredential(session.Home, credential); err != nil {
			result.Warnings = append(result.Warnings, err.Error()+"\n")
		}
	}
	_ = ensureRemoteWorker(session.Home)
	if !persistSession {
		credential.Session = agentSession.Render()
		if err := remote.WriteCredential(session.Home, credential); err != nil {
			return result, err
		}
		if err := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionConfirm}); err != nil {
			reason := issue.Sanitize(err.Error())
			if recordErr := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionUnresolve, Window: credential.Window, Reason: reason}); recordErr != nil {
				result.Warnings = append(result.Warnings, recordErr.Error()+"\n")
			}
			unresolved := &UnresolvedError{TaskID: task.TaskID, Window: credential.Window, Reason: reason, Command: "kander"}
			if plan.OccupiesTerminal() && outcome.Wait != nil {
				unresolved.hold = func() { _, _ = outcome.Wait() }
			}
			return result, unresolved
		}
	}
	return result, nil
}

// withExecutionIdentity passes KANDER_EXECUTION_ID to the agent. A spawned
// process takes it from the environment; a terminal container only receives
// one typed command, so POSIX prefixes `env NAME=value` and PowerShell
// assigns the variable first.
func withExecutionIdentity(plan LaunchPlan, inv process.ProcessInvocation, execution string) process.ProcessInvocation {
	return withInvocationEnvironment(plan, inv, map[string]string{remote.EnvExecutionID: execution, remote.EnvCoordinatorExecutionID: ""})
}
func withInvocationEnvironment(plan LaunchPlan, inv process.ProcessInvocation, values map[string]string) process.ProcessInvocation {
	inv.Env = maps.Clone(inv.Env)
	if inv.Env == nil {
		inv.Env = map[string]string{}
	}
	maps.Copy(inv.Env, values)
	if !plan.capabilities().Container {
		return inv
	}
	if runtimeWindows() {
		inv.ShellEnv = maps.Clone(inv.ShellEnv)
		if inv.ShellEnv == nil {
			inv.ShellEnv = map[string]string{}
		}
		maps.Copy(inv.ShellEnv, values)
		return inv
	}
	prefix := []string{"env"}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		prefix = append(prefix, name+"="+values[name])
	}
	inv.Argv = append(prefix, inv.Argv...)
	return inv
}

// PreviewRemoteStart resolves a server task's local execution configuration
// without claiming it or creating a local card.
func PreviewRemoteStart(ctx context.Context, session *remote.Session, taskID string) (StartPreview, error) {
	task, err := session.Project.Show(ctx, taskID)
	if err != nil {
		return StartPreview{}, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return StartPreview{}, err
	}
	size := board.MetadataFrom(task.Text, board.FieldSize)
	if size != "large" {
		size = "small"
	}
	choice, err := resolveExecution(cfg, task.Text, size, "", execStart)
	if err != nil {
		return StartPreview{}, err
	}
	launcher, err := resolveStartLauncher(cfg.Launcher)
	if err != nil {
		return StartPreview{}, err
	}
	if err := programStatusPreflight(cfg, launcher, choice.Agent); err != nil {
		return StartPreview{}, err
	}
	return StartPreview{TaskID: task.TaskID, State: task.State, Size: size, Agent: choice.Agent, Launcher: launcher}, nil
}
