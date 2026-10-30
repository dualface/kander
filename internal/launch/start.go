package launch

import (
	"time"

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
	problem, err := cardStartProblem(entry, original, func() (config.Rules, error) { return cfg.Rules, nil })
	if err != nil {
		return result, err
	}
	if problem != nil {
		return result, problem
	}
	choice, err := resolveExecution(cfg, original, entry.Kind, agentOverride, execStart)
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
	sessionDef := config.AgentFor(cfg, agentName).Session
	initialSession := session.Render()
	persistSession := config.SessionPersistsAfterStart(sessionDef) || (plan.capabilities().SessionReport && config.SessionDiscoversAfterStart(sessionDef))
	discoverSession := plan.capabilities().PaneMetadata || persistSession
	previous, err := sessionDiscoverSnapshot(sessionDef, entry.TaskID, discoverSession, program, parentDir(root), plan.launchTimeout)
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
	body, err := startAgentPrompt(entry.TaskID, paths, taskGroupFrom(original), original)
	if err != nil {
		return result, err
	}
	taskFile, err := createTaskFile(body, "kander-"+entry.TaskID+"-start-", taskFileTrailer(paths))
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
	prompt, err := headedTaskFileInstruction(paths, "start", entry.TaskID, "", 0, language, taskFile)
	if err != nil {
		return result, err
	}
	args, err := execArguments(choice, session, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	moved, err := beginStartFn(entry, root, board.TaskStartInput{Owner: agentName, Session: initialSession, Window: window, ExecResolved: choice.record})
	if err != nil {
		return result, err
	}
	name := windowName(entry, original)
	var launchStart time.Time
	paneCB := (func() (AgentSession, error))(nil)
	if discoverSession {
		paneCB = func() (AgentSession, error) {
			if session.Reference != "" {
				return session, nil
			}
			if !config.SessionDiscoversAfterStart(sessionDef) {
				return session, nil
			}
			ref, err := runSessionDiscoverHook(sessionDef, moved.TaskID, previous, launchStart, program, parentDir(root), plan.launchTimeout)
			if err != nil {
				return AgentSession{}, err
			}
			return AgentSession{Agent: session.Agent, Reference: ref}, nil
		}
	}
	if persistSession {
		plan.sessionFinalize = func(effective AgentSession) error {
			return board.ConfirmTaskStartSession(root, moved, initialSession, effective.Render())
		}
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.capabilities().Container {
		loc = recordWindowLocation(root, plan, moved, execChoice{})
	}
	launchStart = nowFn()
	outcome, err := launchAgent(plan, root, name, inv, loc, paneCB, &session)
	if err != nil {
		return result, rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &original)
	}
	taskFileHandedOff = true
	if !persistSession {
		err = board.ConfirmTaskStart(root, moved)
		if err != nil {
			return result, err
		}
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
	choice, err := resolveExecution(cfg, snapshot.Text, snapshot.Entry.Kind, "", execStart)
	if err != nil {
		return StartPreview{}, err
	}
	agent := choice.Agent
	launcher, err := resolveStartLauncher(cfg.Launcher)
	if err != nil {
		return StartPreview{}, err
	}
	return StartPreview{TaskID: taskID, State: snapshot.Entry.State, Size: snapshot.Entry.Kind, Agent: agent, Launcher: launcher}, nil
}

// StartProblem runs, read-only, the checks the TUI start path makes for one
// backlog card: the todo gate of the backlog -> todo move, then the card form
// and task-group checks of Start. problem is the first check the card fails;
// err reports that the checks could not finish. Only the task-group check
// reads the effective configuration, so a card problem found earlier never
// depends on it. Nothing is moved, written or launched.
func StartProblem(entry board.Entry, text string) (problem, err error) {
	if problem := board.ValidateTodoContract(entry.Kind, text); problem != nil {
		return problem, nil
	}
	return cardStartProblem(entry, text, func() (config.Rules, error) {
		cfg, err := loadEffective()
		if err != nil {
			return nil, err
		}
		return cfg.Rules, nil
	})
}

// cardStartProblem holds the card checks Start and StartProblem share. rules is
// called only once the card form passed.
func cardStartProblem(entry board.Entry, text string, rules func() (config.Rules, error)) (problem, err error) {
	if problem := board.ValidateMutable(entry, text); problem != nil {
		return problem, nil
	}
	current, err := rules()
	if err != nil {
		return nil, err
	}
	return current.CheckTaskGroup(taskGroupFrom(text)), nil
}
