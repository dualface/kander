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
	if taskID == "" {
		loaded, err := loadBoardFn(root)
		if err != nil {
			return err
		}
		entry, err := selectTodo(loaded, "")
		if err != nil {
			return err
		}
		taskID = entry.TaskID
	}
	result, err := Start(root, agentOverride, launcherOverride, taskID)
	for _, warning := range result.Warnings {
		fmt.Fprint(os.Stderr, warning)
	}
	if err != nil {
		return err
	}
	return reportLaunch(t("launch.started"), board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome)
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

func commandResumeLegacy(root string, agent *string, launcherOverride, taskID, message, messageFile string, messageSet bool, timeout float64, authorization ...resumeBinding) error {
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
	if len(authorization) > 0 {
		bound, e := board.ReadExecutionSnapshot(root, taskID, authorization[0].Authorization)
		if e != nil {
			return e
		}
		entry = bound.Entry
		text = bound.Text
	}
	oldSession, err := sessionFrom(text)
	if err != nil {
		return err
	}
	if err := board.ValidateMutable(entry, text); err != nil {
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
	if !config.HasAgent(cfg, agentName) {
		return launchError("launch.unsupported_agent", agentName)
	}
	if err := applyAgentDelivery(&plan, cfg, agentName); err != nil {
		return err
	}
	program, err := requireAgentProgram(agentName, cfg)
	if err != nil {
		return err
	}
	var session AgentSession
	if takeover {
		session, err = newAgentSession(agentName, program, cfg)
	} else {
		session, err = resolvedTaskSession(entry.TaskID, text, cfg)
	}
	if err != nil {
		return err
	}
	previous := map[string]struct{}{}
	if takeover {
		previous = sessionDiscoverSnapshot(config.AgentFor(cfg, session.Agent).Session.Mode, entry.TaskID, plan.Launcher)
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return err
	}
	var instruction string
	if takeover {
		instruction, err = takeoverAgentPrompt(entry.TaskID, msg, paths, oldSession.Agent, entry.State, text)
	} else {
		instruction, err = resumeAgentPrompt(entry.TaskID, msg, paths, entry.State, text)
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
	args, err := agentArguments(session.Agent, model, entry.Kind, session, !takeover, cfg)
	if err != nil {
		return err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return err
	}
	// resume never moves the card state; a review card is moved back to working by the woken agent itself.
	moved := entry
	effective := session
	paneCB := (func() (AgentSession, error))(nil)
	if plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		mode := config.AgentFor(cfg, session.Agent).Session.Mode
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
			effective = AgentSession{Agent: session.Agent, Reference: ref}
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
	outcome, err := launchAgent(plan, root, windowName(entry, text), inv, loc, paneCB, &session, len(authorization) > 0)
	if err != nil {
		if asLaunchFailure(err).DeliveryUnknown {
			taskFileHandedOff = true
			return err
		}
		return rollbackLaunch(root, moved, entry.State, asLaunchFailure(err), &text)
	}
	taskFileHandedOff = true
	if err := validateResumedDispatch(root, moved, text, plan, outcome, effective, timeout); err != nil {
		if len(authorization) > 0 {
			return err
		}
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
	if len(authorization) > 0 {
		if authorization[0].Outcome != nil {
			*authorization[0].Outcome = ResumeLaunch{Plan: plan, Outcome: outcome}
		}
		return nil
	}
	return reportLaunch(verb, moved, effective.Agent, plan, outcome)
}
