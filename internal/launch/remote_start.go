package launch

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
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
		RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: grant.Claim.RenewedAt,
	}
	if err := session.SaveCredential(credential); err != nil {
		return result, err
	}
	if err := remote.EnsureWorker(session.Home); err != nil {
		result.Warnings = append(result.Warnings, t("remote.worker_start_failed", err.Error())+"\n")
	}
	own := *credential.Claim()
	executionUpdate := func(u api.ExecutionUpdate) error {
		ctx, cancel := call()
		defer cancel()
		_, err := session.Project.Execution(ctx, client.NewKey(), task.TaskID, u, own)
		return err
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
			return executionUpdate(api.ExecutionUpdate{Action: api.ExecutionConfirm, SessionBefore: initialSession, SessionAfter: effective.Render()})
		}
	}
	var location func(LaunchOutcome) error
	if plan.capabilities().Container {
		location = func(outcome LaunchOutcome) error {
			return executionUpdate(api.ExecutionUpdate{Action: api.ExecutionWindow, Window: locationOf(plan, outcome)})
		}
	}
	name := windowName(board.Entry{TaskID: task.TaskID}, original)
	launchStart = nowFn()
	// launchAgent starts the agent in the parent of root; the server-mode
	// root is only that anchor, never a board.
	outcome, err := launchAgent(plan, filepath.Join(project, "kanban"), name, inv, location, paneCB, &agentSession)
	result.TaskID, result.Size, result.Agent = task.TaskID, kind, agentName
	result.Plan, result.Outcome = plan, outcome
	if err != nil {
		failure := asLaunchFailure(err)
		result.Outcome = failure.Outcome
		if rerr := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionRollback}); rerr != nil {
			taskFileHandedOff = true
			return result, launchError("launch.start_and_rollback_both_failed_task_remains_in", "working", failure.Err.Error()+"; "+rerr.Error())
		}
		_ = remote.RemoveCredential(session.Home, credential)
		return result, failure.Err
	}
	taskFileHandedOff = true
	if outcome.Process != nil {
		credential.AgentPID = outcome.Process.Pid
		if err := remote.WriteCredential(session.Home, credential); err != nil {
			result.Warnings = append(result.Warnings, err.Error()+"\n")
		}
	}
	if !persistSession {
		if err := executionUpdate(api.ExecutionUpdate{Action: api.ExecutionConfirm}); err != nil {
			return result, err
		}
	}
	return result, nil
}

// withExecutionIdentity passes KANDER_EXECUTION_ID to the agent. A spawned
// process takes it from the environment; a terminal container only receives
// one typed command, so POSIX prefixes `env NAME=value` and PowerShell
// assigns the variable first.
func withExecutionIdentity(plan LaunchPlan, inv process.ProcessInvocation, execution string) process.ProcessInvocation {
	entry := remote.EnvExecutionID + "=" + execution
	if inv.Env == nil {
		inv.Env = map[string]string{}
	}
	inv.Env[remote.EnvExecutionID] = execution
	if !plan.capabilities().Container {
		return inv
	}
	if runtimeWindows() {
		if inv.ShellEnv == nil {
			inv.ShellEnv = map[string]string{}
		}
		inv.ShellEnv[remote.EnvExecutionID] = execution
		return inv
	}
	inv.Argv = append([]string{"env", entry}, inv.Argv...)
	return inv
}
