package launch

import (
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// StartResult describes a started task. Foreground callers must consume Outcome.Wait.
type StartResult struct {
	TaskID   string
	Size     string
	Agent    string
	Plan     LaunchPlan
	Outcome  LaunchOutcome
	Warnings []string
}

// Start starts one explicit todo task without printing. It preserves the CLI's
// preflight, claim and rollback protocol; the caller owns result presentation.
func Start(root, agentOverride, launcherOverride, taskID string) (result StartResult, err error) {
	if taskID == "" {
		return result, launchError("launch.task_id_is_required")
	}
	var warnings board.WarningLog
	defer func() { result.Warnings = append(warnings.Messages(), result.Warnings...) }()
	loaded, err := board.LoadBoardWithWarnings(root, &warnings)
	if err != nil {
		return result, err
	}
	entry, err := selectTodo(loaded, taskID)
	if err != nil {
		return result, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	original, err := readDocumentFn(entry)
	if err != nil {
		return result, err
	}
	if err := board.ValidateMutable(entry, original); err != nil {
		return result, err
	}
	if err := cfg.Rules.CheckTaskGroup(taskGroupFrom(original)); err != nil {
		return result, err
	}
	agentName := agentOverride
	if agentName == "" {
		agentName, err = config.KanbanAgentFor(cfg, entry.Kind)
		if err != nil {
			return result, err
		}
	}
	if !config.HasAgent(cfg, agentName) {
		return result, launchError("launch.unsupported_agent", agentName)
	}
	launcher := launcherOverride
	if launcher == "" {
		launcher = cfg.Launcher
	}
	plan, err := prepareLaunch(launcher, parentDir(root), "start")
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
	session, err := newAgentSession(agentName, program, cfg)
	if err != nil {
		return result, err
	}
	previous := sessionDiscoverSnapshot(config.AgentFor(cfg, agentName).Session.Mode, entry.TaskID, plan.Launcher)
	window := ""
	if plan.Launcher == "foreground" || plan.Launcher == "console" {
		window = plan.Launcher
	}
	updated, err := startMetadata(original, agentName, session, window)
	if err != nil {
		return result, err
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return result, err
	}
	body, err := startAgentPrompt(entry.TaskID, paths, taskGroupFrom(original), original)
	if err != nil {
		return result, err
	}
	taskFile, err := createTaskFile(body, "kander-"+entry.TaskID+"-start-")
	if err != nil {
		return result, err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.start_head", entry.TaskID), taskFile)
	model := cfg.Models.Kanban[agentName]
	args, err := agentArguments(agentName, model, entry.Kind, session, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	moved, err := moveEntryFn(entry, root, "working")
	if err != nil {
		return result, err
	}
	name := windowName(entry, original)
	paneCB := (func() (AgentSession, error))(nil)
	if plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		mode := config.AgentFor(cfg, agentName).Session.Mode
		paneCB = func() (AgentSession, error) {
			if session.Reference != "" {
				return session, nil
			}
			if !config.SessionDiscoversAfterStart(mode) {
				return session, nil
			}
			ref, err := runSessionDiscoverHook(mode, moved.TaskID, previous)
			if err != nil {
				return AgentSession{}, err
			}
			return AgentSession{Agent: session.Agent, Reference: ref}, nil
		}
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.Launcher == "herdr" || plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		loc = recordWindowLocation(root, plan, moved)
	}
	if err := writeDocumentFn(root, moved, updated); err != nil {
		return result, rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &original)
	}
	outcome, err := launchAgent(plan, root, name, inv, loc, paneCB, &session)
	if err != nil {
		return result, rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &original)
	}
	taskFileHandedOff = true
	if err = board.ConfirmTaskStart(root, moved); err != nil {
		return result, err
	}
	result.TaskID, result.Size, result.Agent = moved.TaskID, moved.Kind, agentName
	result.Plan, result.Outcome = plan, outcome
	return result, nil
}

// StartPreview contains read-only defaults for a start confirmation.
type StartPreview struct {
	Warnings []string
	TaskID   string
	State    string
	Size     string
	Agent    string
	Launcher string
}

// PreviewStart resolves task size, configured agent and launcher without claiming
// a task, allocating a session or creating a terminal container.
func PreviewStart(root, taskID string) (preview StartPreview, err error) {
	var warnings board.WarningLog
	defer func() { preview.Warnings = warnings.Messages() }()
	snapshot, err := board.ReadSnapshotWithWarnings(root, taskID, &warnings)
	if err != nil {
		return StartPreview{}, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return StartPreview{}, err
	}
	agent, err := config.KanbanAgentFor(cfg, snapshot.Entry.Kind)
	if err != nil {
		return StartPreview{}, err
	}
	launcher, err := resolveStartLauncher(cfg.Launcher)
	if err != nil {
		return StartPreview{}, err
	}
	return StartPreview{TaskID: taskID, State: snapshot.Entry.State, Size: snapshot.Entry.Kind, Agent: agent, Launcher: launcher}, nil
}
