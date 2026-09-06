package launch

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func selectTodo(b board.Board, task string) (board.Entry, error) {
	if task != "" {
		entry, err := locateFn(b, task)
		if err != nil {
			return board.Entry{}, err
		}
		if entry.State != "todo" {
			return board.Entry{}, launchError(
				"launch.start_only_accepts_tasks_in_todo_is_in", entry.TaskID, entry.State,
			)
		}
		return entry, nil
	}
	var candidates []board.Entry
	for _, entry := range b.Entries {
		if entry.State == "todo" {
			candidates = append(candidates, entry)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TaskID < candidates[j].TaskID })
	if len(candidates) == 0 {
		return board.Entry{}, launchError("launch.no_tasks_in_todo")
	}
	for i, entry := range candidates {
		text, err := readDocumentFn(entry)
		if err != nil {
			return board.Entry{}, err
		}
		fmt.Printf("%d. %s\t%s\n", i+1, entry.TaskID, board.TitleFrom(text))
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(t("board.choose_a_task_number"))
		line, err := reader.ReadString('\n')
		if err != nil {
			return board.Entry{}, launchError("board.no_task_selected_specify_task_id")
		}
		choice := strings.TrimSpace(line)
		n, convErr := strconv.Atoi(choice)
		if convErr == nil && n >= 1 && n <= len(candidates) {
			return candidates[n-1], nil
		}
		fmt.Print(t("board.enter_a_number_from_1_to", strconv.Itoa(len(candidates))))
	}
}

func commandStart(root, agentOverride, launcherOverride, taskID string) error {
	loaded, err := loadBoardFn(root)
	if err != nil {
		return err
	}
	entry, err := selectTodo(loaded, taskID)
	if err != nil {
		return err
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	original, err := readDocumentFn(entry)
	if err != nil {
		return err
	}
	if err := cfg.Rules.CheckTaskGroup(taskGroupFrom(original)); err != nil {
		return err
	}
	agentName := agentOverride
	if agentName == "" {
		agentName, err = config.KanbanAgentFor(cfg, entry.Kind)
		if err != nil {
			return err
		}
	}
	if !contains(config.ExecutionAgents, agentName) {
		return launchError("launch.unsupported_agent", agentName)
	}
	launcher := launcherOverride
	if launcher == "" {
		launcher = cfg.Launcher
	}
	plan, err := prepareLaunch(launcher, parentDir(root), "start")
	if err != nil {
		return err
	}
	program, err := requireAgentProgram(agentName)
	if err != nil {
		return err
	}
	session, err := newAgentSession(agentName, program)
	if err != nil {
		return err
	}
	previous := map[string]struct{}{}
	if agentName == "codex" && (plan.Launcher == "tmux" || plan.Launcher == "tmux-session") {
		if sessions, err := codexSessionsForTask(entry.TaskID); err == nil {
			for _, id := range sessions {
				previous[id] = struct{}{}
			}
		}
	}
	window := ""
	if plan.Launcher == "foreground" || plan.Launcher == "console" {
		window = plan.Launcher
	}
	updated, err := startMetadata(original, agentName, session, window)
	if err != nil {
		return err
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return err
	}
	body, err := startAgentPrompt(entry.TaskID, paths, taskGroupFrom(original))
	if err != nil {
		return err
	}
	taskFile, err := createTaskFile(body, "kander-"+entry.TaskID+"-start-")
	if err != nil {
		return err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.start_head", entry.TaskID), taskFile)
	model := cfg.Models.Kanban[agentName]
	args, err := agentArguments(agentName, model, entry.Kind, session, false)
	if err != nil {
		return err
	}
	inv, err := launchInvocation(plan, *program, append(args, prompt))
	if err != nil {
		return err
	}
	moved, err := moveEntryFn(entry, root, "working")
	if err != nil {
		return err
	}
	name := windowName(entry, original)
	paneCB := (func() (AgentSession, error))(nil)
	if plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		paneCB = func() (AgentSession, error) {
			if session.Reference != "" {
				return session, nil
			}
			ref, err := discoverNewCodexSession(moved.TaskID, previous)
			if err != nil {
				return AgentSession{}, err
			}
			return AgentSession{Agent: "codex", Reference: ref}, nil
		}
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.Launcher == "herdr" || plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		loc = recordWindowLocation(root, plan, moved)
	}
	if err := writeDocumentFn(root, moved, updated); err != nil {
		return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &original)
	}
	outcome, err := launchAgent(plan, root, name, inv, loc, paneCB, &session)
	if err != nil {
		return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &original)
	}
	taskFileHandedOff = true
	return reportLaunch(t("launch.started"), moved, agentName, plan, outcome)
}

func parentDir(p string) string {
	dir := p
	if i := lastSlash(dir); i >= 0 {
		return dir[:i]
	}
	return dir
}

func lastSlash(p string) int {
	i := strings.LastIndex(p, "/")
	j := strings.LastIndex(p, `\`)
	if j > i {
		return j
	}
	return i
}

func commandResume(root string, agent *string, launcherOverride, taskID, message, messageFile string, messageSet bool, timeout float64) error {
	if err := validateLivenessTimeout(timeout, "resume"); err != nil {
		return err
	}
	loaded, err := loadBoardFn(root)
	if err != nil {
		return err
	}
	entry, err := locateFn(loaded, taskID)
	if err != nil {
		return err
	}
	if entry.State != "review" && entry.State != "working" {
		return launchError(
			"launch.only_tasks_in_review_or_working_can_be_resumed", entry.TaskID, entry.State,
		)
	}
	msg, err := readTaskMessage(message, messageSet, messageFile, "resume")
	if err != nil {
		return err
	}
	text, err := readDocumentFn(entry)
	if err != nil {
		return err
	}
	oldSession, err := sessionFrom(text)
	if err != nil {
		return err
	}
	oldWindow := metadataFrom(text, windowField)
	takeover := agent != nil
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	if err := cfg.Rules.CheckTaskGroup(taskGroupFrom(text)); err != nil {
		return err
	}
	launcher := launcherOverride
	if launcher == "" {
		launcher = cfg.Launcher
	}
	plan, err := prepareLaunch(launcher, parentDir(root), "resume")
	if err != nil {
		return err
	}
	agentName := oldSession.Agent
	if takeover {
		agentName = *agent
	}
	program, err := requireAgentProgram(agentName)
	if err != nil {
		return err
	}
	var session AgentSession
	if takeover {
		session, err = newAgentSession(agentName, program)
	} else {
		session, err = resolvedTaskSession(entry.TaskID, text)
	}
	if err != nil {
		return err
	}
	previous := map[string]struct{}{}
	if takeover && session.Agent == "codex" && (plan.Launcher == "tmux" || plan.Launcher == "tmux-session") {
		if sessions, err := codexSessionsForTask(entry.TaskID); err == nil {
			for _, id := range sessions {
				previous[id] = struct{}{}
			}
		}
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return err
	}
	var instruction string
	if takeover {
		instruction, err = takeoverAgentPrompt(entry.TaskID, msg, paths, oldSession.Agent, entry.State)
	} else {
		instruction, err = resumeAgentPrompt(entry.TaskID, msg, paths, entry.State)
	}
	if err != nil {
		return err
	}
	prefix := "kander-" + entry.TaskID + "-resume-"
	if takeover {
		prefix = "kander-" + entry.TaskID + "-takeover-"
	}
	taskFile, err := createTaskFile(instruction, prefix)
	if err != nil {
		return err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	head := t("launch.prompt.resume_head", entry.TaskID)
	if takeover {
		head = t("launch.prompt.takeover_head", entry.TaskID)
	}
	prompt := taskInstruction(head, taskFile)
	model := cfg.Models.Kanban[session.Agent]
	args, err := agentArguments(session.Agent, model, entry.Kind, session, !takeover)
	if err != nil {
		return err
	}
	inv, err := launchInvocation(plan, *program, append(args, prompt))
	if err != nil {
		return err
	}
	// resume never moves the card state; a review card is moved back to working by the woken agent itself.
	moved := entry
	effective := session
	paneCB := (func() (AgentSession, error))(nil)
	if plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		paneCB = func() (AgentSession, error) {
			if session.Reference != "" {
				return session, nil
			}
			ref, err := discoverNewCodexSession(moved.TaskID, previous)
			if err != nil {
				return AgentSession{}, err
			}
			effective = AgentSession{Agent: "codex", Reference: ref}
			if takeover {
				current, err := readDocumentFn(moved)
				if err != nil {
					return AgentSession{}, err
				}
				updated, err := renderSessionMetadata(current, effective.Render())
				if err != nil {
					return AgentSession{}, err
				}
				if err := writeDocumentFn(root, moved, updated); err != nil {
					return AgentSession{}, err
				}
			}
			return effective, nil
		}
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.Launcher == "herdr" || plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		loc = recordWindowLocation(root, plan, moved)
	}
	if takeover {
		current, err := readDocumentFn(moved)
		if err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
		window := ""
		if plan.Launcher == "foreground" || plan.Launcher == "console" {
			window = plan.Launcher
		}
		updated, err := renderTakeoverMetadata(current, session.Agent, session.Render(), window)
		if err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
		if err := writeDocumentFn(root, moved, updated); err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
	} else if plan.Launcher == "foreground" || plan.Launcher == "console" {
		current, err := readDocumentFn(moved)
		if err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
		updated, err := windowMetadata(current, plan.Launcher)
		if err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
		if err := writeDocumentFn(root, moved, updated); err != nil {
			return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
		}
	}
	outcome, err := launchAgent(plan, root, windowName(entry, text), inv, loc, paneCB, &session)
	if err != nil {
		return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
	}
	taskFileHandedOff = true
	if err := validateResumedAgent(plan, outcome, effective, timeout); err != nil {
		detail := resumedAgentFailureOutput(plan, outcome)
		if detail != "" {
			err = launchError("launch.agent_output", err.Error(), detail)
		}
		var cleanup string
		if cErr := cleanupFailedResume(plan, outcome); cErr != nil {
			cleanup = cErr.Error()
		}
		return rollbackLaunch(root, moved, entry.State, &LaunchFailure{Err: err, CloseError: cleanup}, &text)
	}
	if takeover {
		cleanup := naCleanup()
		if CleanupTakeover != nil {
			newWindow := oldWindow
			if current, err := readDocumentFn(moved); err == nil {
				newWindow = metadataFrom(current, windowField)
			} else {
				cleanup = CleanupResult{Cleaned: false, OldWindow: orNA(oldWindow), Detail: strings.Join(strings.Fields(err.Error()), " ")}
			}
			if cleanup.Detail == "" {
				cleanup = CleanupTakeover(oldWindow, oldSession, newWindow, timeout)
			}
		}
		if cleanup.Cleaned {
			fmt.Println(t(
				"launch.cleaned_old_container_channel_closed_container", cleanup.OldWindow, cleanup.Channel, cleanup.Container,
			))
		} else {
			fmt.Println(t(
				"launch.old_container_retained_reason", cleanup.OldWindow, cleanup.Detail,
			))
		}
	}
	verb := t("launch.resumed")
	if takeover {
		verb = t("launch.taken_over")
	}
	return reportLaunch(verb, moved, effective.Agent, plan, outcome)
}
