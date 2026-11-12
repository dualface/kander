package launch

import (
	"context"
	"encoding/json"
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
func StartRemote(agentOverride, launcherOverride, taskID string) (StartResult, error) {
	return launchRemote(remoteLaunchInput{Agent: agentOverride, Launcher: launcherOverride, TaskID: taskID})
}

func launchRemote(input remoteLaunchInput) (result StartResult, err error) {
	agentOverride, launcherOverride, taskID := input.Agent, input.Launcher, input.TaskID
	verb := "start"
	if input.Takeover {
		verb = "resume"
	}
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
	begin := session.BeginAction
	if input.Native {
		begin = session.RetainAction
	}
	journal, fresh, err := begin("task-launch", struct {
		Task  string
		Input remoteLaunchInput
	}{task.TaskID, input})
	if err != nil {
		return result, err
	}
	defer journal.Close()
	if !fresh && journal.Value.Phase == "complete" && len(journal.Value.Result) != 0 {
		return completedRemoteLaunch(journal)
	}
	if !fresh && journal.Value.Phase == "failed" {
		return result, launchError("remote.native_attempt_settled", journal.Path)
	}
	if !fresh && journal.Value.Phase != "failed" && journal.Value.Phase != "prepared" && journal.Value.Phase != "session-ready" {
		ctx, cancel := call()
		defer cancel()
		return result, recoverRemoteLaunch(ctx, session, journal)
	}
	if !input.Takeover && task.State != "todo" {
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
	var restart remoteRestart
	if input.Takeover {
		ctx, cancel := call()
		restart, err = prepareRemoteRestart(ctx, session, task, cwd, input)
		cancel()
		if err != nil {
			return result, err
		}
		if input.Native {
			agentOverride = restart.Session.Agent
		}
	}
	mode := execStart
	if input.Takeover {
		mode = execTakeover
	}
	if input.Native {
		mode = execContinue
	}
	choice, err := resolveExecution(cfg, original, kind, agentOverride, mode)
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
	plan, err := prepareLaunch(launcher, project, verb)
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
	agentSession := restart.Session
	if !input.Native {
		agentSession, err = journalAgentSession(journal, agentName, program, cfg)
	}
	if input.Native {
		if err := refuseNativeResume(task.TaskID, original, plan); err != nil {
			return result, err
		}
	}
	if err != nil {
		return result, err
	}
	sessionDef := config.AgentFor(cfg, agentName).Session
	initialSession := agentSession.Render()
	persistSession := !input.Native && (config.SessionPersistsAfterStart(sessionDef) || (plan.capabilities().SessionReport && config.SessionDiscoversAfterStart(sessionDef)))
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
	var body string
	if input.Takeover {
		body, err = takeoverAgentPrompt(task.TaskID, input.Message, paths, board.MetadataFrom(original, board.FieldOwner), task.State, original)
	} else {
		body, err = startAgentPrompt(task.TaskID, paths, taskGroupFrom(original), original)
	}
	if err != nil {
		return result, err
	}
	if input.Takeover {
		body = "Read the current task from the configured server before writing. The embedded card is a pre-handoff snapshot. Reconcile existing receipts and pending external effects before continuing; never repeat an already accepted or completed action blindly.\n\n" + body
		if restart.Dispatch != nil {
			if restart.Request.Dispatch.Mode == "settle-completed" {
				body += "\nThe previous dispatch is completed and is settled atomically with this handoff. Do not reuse its authorization or repeat its work.\n"
			} else {
				instruction, instructionErr := DispatchInstruction(paths, *restart.Dispatch)
				if instructionErr != nil {
					return result, instructionErr
				}
				body += "\n\n" + instruction + "\nOriginal dispatch message:\n" + restart.Dispatch.Input.Message + "\n"
			}
		}
		if input.PendingRequest != "" {
			body = "Begin with read-only reconciliation. Wait for this generation's proposal before any new code changes or external actions: kander request show " + task.TaskID + " " + input.PendingRequest + " --json. It is published after this session is confirmed. Inspect its version and generation, prepare it through the controlled dispatch producer, and accept its business authorization before starting the new round. If it remains absent or is superseded, report the facts and stop; do not invent a replacement.\n\n" + body
		}
	}
	if session.Info.Remote != "" {
		body += "\n\nThe current directory is the worktree and branch prepared for this execution generation. Use this worktree for implementation and keep all predecessor worktrees intact. Task documents and execution authority belong to the configured server project; do not create a local kanban directory.\n"
	}
	taskFile, err := createTaskFile(body, "kander-"+task.TaskID+"-"+verb+"-", taskFileTrailer(paths))
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
	prompt, err := headedTaskFileInstruction(paths, verb, task.TaskID, "", 0, language, taskFile)
	if err != nil {
		return result, err
	}
	args, err := execArguments(choice, agentSession, input.Native && sessionDef.Mode != "none", cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	execution := journal.Value.ExecutionID
	result.executionID = execution
	inv = withExecutionIdentity(plan, inv, execution)

	ctx, cancel = call()
	if restart.Continuation != nil {
		if err := remote.VerifyContinuation(ctx, restart.SourceDir, *restart.Continuation); err != nil {
			cancel()
			return result, err
		}
	}
	if restart.Proof != nil {
		state, verifyErr := session.Project.ExecutionState(ctx, task.TaskID, *restart.Proof)
		if verifyErr != nil {
			cancel()
			return result, verifyErr
		}
		if state.Task.Revision != task.Revision {
			cancel()
			return result, &api.Error{Code: api.CodeRevisionConflict, Message: t("remote.delivery_card_changed")}
		}
		presence := observeLocalExecution(ctx, *restart.Subject)
		if presence.Status != "stopped" {
			cancel()
			return result, launchError("remote.restart_needs_stopped")
		}
		restart.Request.ObservationTicket, restart.Request.PreviousPresence = state.ObservationTicket, &presence
	}
	request := api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: execution, Owner: agentName, ExpectedRevision: task.Revision, Start: &api.StartInput{Session: initialSession, Window: window, ExecResolved: choice.record}}
	var grant api.ClaimGrant
	if input.Takeover {
		request.Decision, request.Dispatch, request.ExpectedGeneration = restart.Request.Decision, restart.Request.Dispatch, restart.Request.ExpectedGeneration
		request.ObservationTicket, request.PreviousPresence = restart.Request.ObservationTicket, restart.Request.PreviousPresence
		if err := freezeRemoteLaunch(journal, task.TaskID, request, restart.Proof, true, taskFile); err != nil {
			cancel()
			return result, err
		}
		if restart.Proof != nil {
			grant, err = session.Project.Handoff(ctx, journal.Value.ClaimKey, task.TaskID, request, *restart.Proof)
		} else {
			grant, err = session.Project.Takeover(ctx, journal.Value.ClaimKey, task.TaskID, request)
		}
	} else {
		if err := freezeRemoteLaunch(journal, task.TaskID, request, nil, false, taskFile); err != nil {
			cancel()
			return result, err
		}
		grant, err = session.Project.Claim(ctx, journal.Value.ClaimKey, task.TaskID, request)
	}
	cancel()
	if err != nil {
		return result, err
	}
	credential := remote.Credential{
		ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: grant.Task.TaskID,
		ExecutionID: execution, Generation: grant.Claim.Generation, Token: grant.Token,
		RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: time.Now().UTC(), Session: initialSession, Window: window,
	}
	if err := session.SaveCredential(credential); err != nil {
		return result, err
	}
	if err := ensureRemoteWorker(session.Home); err != nil {
		result.Warnings = append(result.Warnings, t("remote.worker_start_failed", err.Error())+"\n")
	}
	own := *credential.Claim()
	executionUpdate := func(u api.ExecutionUpdate) error {
		ctx, cancel := call()
		defer cancel()
		_, err := session.Project.Execution(ctx, client.NewKey(), task.TaskID, u, own)
		return err
	}
	if session.Info.Remote != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		sourceDir := cwd
		if restart.SourceDir != "" {
			sourceDir = restart.SourceDir
		}
		baseline, workspaceErr := session.PublishBaseline(ctx, sourceDir, task.TaskID, restart.Base, credential)
		var workspace remote.ExecutionWorktree
		if workspaceErr == nil {
			workspace, workspaceErr = session.PrepareWorktree(ctx, sourceDir, baseline.Commit, credential)
		}
		if workspaceErr == nil && restart.Continuation != nil {
			workspaceErr = session.RestoreContinuation(ctx, workspace, *restart.Continuation, credential)
		}
		cancel()
		if workspaceErr == nil {
			project = workspace.Path
			credential.Worktree, credential.Branch, credential.BaseCommit = workspace.Path, workspace.Branch, workspace.Base
			plan, workspaceErr = prepareLaunch(launcher, project, verb)
		}
		if workspaceErr == nil {
			workspaceErr = applyAgentDelivery(&plan, cfg, agentName)
		}
		if workspaceErr == nil {
			plan.warning = func(message string) { result.Warnings = append(result.Warnings, message) }
			inv, workspaceErr = launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
			inv = withExecutionIdentity(plan, inv, execution)
		}
		if workspaceErr == nil {
			workspaceErr = executionUpdate(api.ExecutionUpdate{Action: api.ExecutionWorkspace, Branch: workspace.Branch, ExpectedRevision: grant.Task.Revision})
		}
		if workspaceErr == nil {
			previous, workspaceErr = sessionDiscoverSnapshot(sessionDef, task.TaskID, discoverSession, program, project, plan.launchTimeout)
		}
		if workspaceErr != nil {
			ctx, cancel := call()
			rollbackErr := abandonUnlaunchedGeneration(ctx, session, credential, input.Takeover, journal)
			cancel()
			if rollbackErr != nil {
				return result, errors.Join(workspaceErr, rollbackErr)
			}
			return result, workspaceErr
		}
	}
	promptAttempted := false
	plan.beforePrompt = func() error { promptAttempted = true; journal.Value.Phase = "prompt-attempted"; return journal.Save() }
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
			journal.Value.Session = credential.Session
			if err := journal.Save(); err != nil {
				return err
			}
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
			journal.Value.Window = credential.Window
			if err := journal.Save(); err != nil {
				return err
			}
			if err := remote.WriteCredential(session.Home, credential); err != nil {
				return err
			}
			return executionUpdate(api.ExecutionUpdate{Action: api.ExecutionWindow, Window: credential.Window})
		}
	}
	journal.Value.Phase = "launching"
	if err := journal.Save(); err != nil {
		return result, err
	}
	name := windowName(board.Entry{TaskID: task.TaskID}, original)
	launchStart = nowFn()
	// launchAgent starts the agent in the parent of root; the server-mode
	// root is only that anchor, never a board.
	outcome, err := launchAgent(plan, filepath.Join(project, "task-context"), name, inv, location, paneCB, &agentSession, true)
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
			ctx, cancel := call()
			rollbackErr := abandonUnlaunchedGeneration(ctx, session, credential, input.Takeover, journal)
			cancel()
			if rollbackErr == nil {
				return result, failure.Err
			}
			err = errors.Join(failure.Err, rollbackErr)
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
	journal.Value.Result, err = json.Marshal(taskLaunchReceipt{TaskID: task.TaskID, Size: kind, Agent: agentName, Launcher: plan.Launcher, Target: plan.Target, Container: outcome.Container, Pane: outcome.Pane, SessionMarked: outcome.SessionMarked})
	if err != nil {
		return result, err
	}
	journal.Value.Phase = "complete"
	if err := journal.Save(); err != nil {
		return result, &UnresolvedError{TaskID: task.TaskID, Window: credential.Window, Reason: err.Error(), Command: "kander"}
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
