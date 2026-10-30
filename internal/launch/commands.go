package launch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

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
	if result.Superseded {
		stepAside(result.TaskID, result.Plan, result.Outcome)
		return nil
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

func commandResumeLegacy(root string, agent *string, launcherOverride, taskID, message, messageFile string, messageSet bool, timeout float64, binding resumeBinding) error {
	durable := binding.Authorization != (board.ExecutionAuthorization{})
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
	// A snapshot pins the execution this resume acts for; a scan entry pins none.
	current, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		return err
	}
	entry = current.Entry
	text := current.Text
	if durable {
		bound, e := board.ReadExecutionSnapshot(root, taskID, binding.Authorization)
		if e != nil {
			return e
		}
		entry = bound.Entry
		text = bound.Text
	}
	// A self-claimed card has no session; a takeover replaces the claiming owner.
	oldSession := AgentSession{Agent: metadataFrom(text, board.FieldOwner)}
	if agent == nil || metadataFrom(text, sessionField) != "" {
		if oldSession, err = sessionFrom(text); err != nil {
			return err
		}
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
	agentName, mode := oldSession.Agent, execContinue
	if takeover {
		agentName, mode = *agent, execTakeover
	}
	choice, err := resolveExecution(cfg, text, entry.Kind, agentName, mode)
	if err != nil {
		return err
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
	sessionDef := config.AgentFor(cfg, session.Agent).Session
	discoverSession := plan.capabilities().PaneMetadata || plan.capabilities().SessionReport || config.SessionPersistsAfterStart(sessionDef)
	if takeover {
		previous, err = sessionDiscoverSnapshot(sessionDef, entry.TaskID, discoverSession, program, parentDir(root), plan.launchTimeout)
		if err != nil {
			return err
		}
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
	taskFile, err := createTaskFile(instruction, prefix, taskFileTrailer(paths))
	if err != nil {
		return err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	kind := "resume"
	if takeover {
		kind = "takeover"
	}
	language, err := resolvePromptLanguage(text, paths)
	if err != nil {
		return err
	}
	prompt, err := headedTaskFileInstruction(paths, kind, entry.TaskID, "", 0, language, taskFile)
	if err != nil {
		return err
	}
	args, err := execArguments(choice, session, !takeover, cfg)
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
	if _, err := board.BindExecutionCursor(root, moved); err != nil {
		return err
	}
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
			effective = AgentSession{Agent: session.Agent, Reference: ref}
			if takeover || config.SessionPersistsAfterStart(sessionDef) {
				before := session.Render()
				if !takeover {
					before = metadataFrom(text, sessionField)
				}
				if _, err := board.RecordExecutionSession(context.Background(), root, moved, before, effective.Render()); err != nil {
					return AgentSession{}, err
				}
			}
			return effective, nil
		}
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.capabilities().Container {
		loc = recordWindowLocation(root, plan, moved, choice)
	}
	if takeover {
		window := ""
		if !plan.capabilities().Container {
			window = plan.Launcher
		}
		if _, err := beginTakeoverFn(root, moved, board.TakeoverInput{Owner: session.Agent, Session: session.Render(), Window: window, ExecResolved: choice.record, Decision: binding.Decision}); err != nil {
			return err
		}
		plan.judge = lateFailureJudge(root, moved)
	} else if !plan.capabilities().Container {
		if _, err := recordWindowFn(root, moved, plan.Launcher, choice.record); err != nil {
			return err
		}
	}
	launchStart = nowFn()
	// settled ends a takeover that failed, and reports a takeover another one
	// overtook as a normal exit.
	settled := func(outcome LaunchOutcome, err error) error {
		if errors.Is(err, board.ErrExecutionSuperseded) {
			stepAside(entry.TaskID, plan, outcome)
			return nil
		}
		return err
	}
	outcome, err := launchAgent(plan, root, windowName(entry, text), inv, loc, paneCB, &session, durable)
	if err != nil {
		failure := asLaunchFailure(err)
		switch {
		case failure.DeliveryUnknown && takeover:
			taskFileHandedOff = true
			return settled(failure.Outcome, recordUnresolved(root, plan, moved, failure.Outcome, failure.Err))
		case failure.DeliveryUnknown:
			taskFileHandedOff = true
			return err
		case takeover:
			err = settleFailedLaunch(root, plan, moved, entry.State, failure, text)
			taskFileHandedOff = err != failure.Err
			return settled(failure.Outcome, err)
		}
		return rollbackLaunch(root, moved, entry.State, failure, &text)
	}
	taskFileHandedOff = true
	if err := validateResumedDispatch(root, moved, text, plan, outcome, effective, timeout); err != nil {
		if durable && takeover {
			return settled(outcome, recordUnresolved(root, plan, moved, outcome, err))
		}
		if durable {
			return err
		}
		detail := resumedAgentFailureOutput(plan, outcome)
		if detail != "" {
			err = launchError("launch.agent_output", err.Error(), detail)
		}
		if takeover {
			return settled(outcome, settleAfterLaunch(root, plan, moved, entry.State, outcome, err, text))
		}
		var cleanup string
		if cErr := cleanupFailedResume(plan, outcome); cErr != nil {
			cleanup = cErr.Error()
		}
		return rollbackLaunch(root, moved, entry.State, &LaunchFailure{Err: err, CloseError: cleanup}, &text)
	}
	if takeover {
		if err := board.ConfirmTaskStart(root, moved); err != nil {
			return settled(outcome, settleAfterLaunch(root, plan, moved, entry.State, outcome, err, text))
		}
		cleanup := naCleanup()
		if CleanupTakeover != nil {
			newWindow := oldWindow
			if current, err := board.ReadSnapshot(root, moved.TaskID); err == nil {
				newWindow = metadataFrom(current.Text, windowField)
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
	if durable {
		if binding.Outcome != nil {
			*binding.Outcome = ResumeLaunch{Plan: plan, Outcome: outcome}
		}
		return nil
	}
	return reportLaunch(verb, moved, effective.Agent, plan, outcome)
}
