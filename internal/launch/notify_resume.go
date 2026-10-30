package launch

import (
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/window"
)

// ResumeLaunch is the launcher plus the process/terminal address after the notify recovery channel succeeds.
type ResumeLaunch struct {
	Plan    LaunchPlan
	Outcome LaunchOutcome
}

// ValidateTimeout requires timeout to be a finite number of seconds greater than 60.
func ValidateTimeout(timeout float64, command string) error {
	return validateLivenessTimeout(timeout, command)
}

// ReadMessage reads --message or --message-file; exactly one of them must be given.
func ReadMessage(message string, messageSet bool, messageFile, command string) (string, error) {
	return readTaskMessage(message, messageSet, messageFile, command)
}

// RequireSessionExecution refuses a card no launcher started. Such a card has no
// session to resume, notify or dismiss, and the refusal comes before any dispatch
// binds. resume --agent never calls it: a takeover needs no old session.
func RequireSessionExecution(root, task string) error {
	record, exists, err := board.CurrentExecution(root, task)
	if err != nil {
		return err
	}
	if exists && !record.ExecutionTerminal() && record.SelfClaimed() {
		return launchError("launch.execution_has_no_session", task, record.Kind)
	}
	return nil
}

// ResolvedSession resolves the card session; Codex cards without an id go through rollout lookup.
func ResolvedSession(taskID, text string) (AgentSession, error) {
	return resolvedTaskSession(taskID, text)
}

// ResolvedSessionIdentity resolves terminal identity independently of resume support.
func ResolvedSessionIdentity(taskID, text string) (AgentSession, error) {
	return resolveTaskIdentity(taskID, text, false)
}

// NotifyViaResume resumes the original session, or starts fresh for session.mode=none.
// Legacy failures and durable failures before a send attempt restore the pre-call
// text only while this operation owns the current revision and authorization;
// otherwise newer records are preserved and a conflict is reported. After an
// uncertain durable send or post-launch validation failure, preserve the executor,
// WINDOW, text and task file so the caller can reconcile durable receipts.
func NotifyViaResume(root string, entry board.Entry, originalText, message string, timeout float64) (ResumeLaunch, error) {
	if err := board.ValidateMutable(entry, originalText); err != nil {
		return ResumeLaunch{}, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return ResumeLaunch{}, err
	}
	originalSession, err := sessionFrom(originalText)
	if err != nil {
		return ResumeLaunch{}, err
	}
	definition := config.AgentFor(cfg, originalSession.Agent)
	resume := definition.Session.Mode != "none"
	var session AgentSession
	if resume {
		session, err = resolvedTaskSession(entry.TaskID, originalText, cfg)
	} else {
		session = originalSession
	}
	if err != nil {
		return ResumeLaunch{}, err
	}
	if err := cfg.Rules.CheckTaskGroup(taskGroupFrom(originalText)); err != nil {
		return ResumeLaunch{}, err
	}
	choice, err := resolveExecution(cfg, originalText, entry.Kind, session.Agent, execContinue)
	if err != nil {
		return ResumeLaunch{}, err
	}
	plan, err := prepareLaunch(cfg.Launcher, parentDir(root), "notify")
	if err != nil {
		return ResumeLaunch{}, err
	}
	if err := applyAgentDelivery(&plan, cfg, session.Agent); err != nil {
		return ResumeLaunch{}, err
	}
	program, err := requireAgentProgram(session.Agent, cfg)
	if err != nil {
		return ResumeLaunch{}, err
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return ResumeLaunch{}, err
	}
	promptBody, err := resumePrompt(entry.TaskID, message, paths, entry.State, originalText)
	if err != nil {
		return ResumeLaunch{}, err
	}
	taskFile, err := createTaskFile(promptBody, "kander-"+entry.TaskID+"-notify-", taskFileTrailer(paths))
	if err != nil {
		return ResumeLaunch{}, err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	language, err := resolvePromptLanguage(originalText, paths)
	if err != nil {
		return ResumeLaunch{}, err
	}
	prompt, err := headedTaskFileInstruction(paths, "resume", entry.TaskID, "", 0, language, taskFile)
	if err != nil {
		return ResumeLaunch{}, err
	}
	args, err := execArguments(choice, session, resume, cfg)
	if err != nil {
		return ResumeLaunch{}, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return ResumeLaunch{}, err
	}
	record, err := board.BindExecutionCursor(root, entry)
	if err != nil {
		return ResumeLaunch{}, err
	}
	if err := refuseNativeResume(entry.TaskID, originalText, record, plan); err != nil {
		return ResumeLaunch{}, err
	}
	plan.processRecord = processRecorder(root, entry, plan)
	if !plan.capabilities().Container {
		if _, err := recordWindowFn(root, entry, plan.Launcher, choice.record); err != nil {
			return ResumeLaunch{}, err
		}
	}
	paneCB := (func() (AgentSession, error))(nil)
	if plan.capabilities().PaneMetadata {
		paneCB = func() (AgentSession, error) { return session, nil }
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.capabilities().Container {
		loc = recordWindowLocation(root, plan, entry, choice)
	}
	durable := board.MetadataFrom(originalText, "DISPATCH_ID") != ""
	outcome, err := launchAgent(plan, root, windowName(entry, originalText), inv, loc, paneCB, &session, durable)
	if err != nil {
		failure := asLaunchFailure(err)
		if failure.DeliveryUnknown {
			taskFileHandedOff = true
			return ResumeLaunch{}, err
		}
		rollback := window.RestoreWindowText(root, entry, originalText)
		if msg := window.ResumeFailureMessage(failure.Err, errorString(failure.CloseError), rollback); msg != "" {
			return ResumeLaunch{}, &Error{Message: msg}
		}
		return ResumeLaunch{}, err
	}
	taskFileHandedOff = true
	if err := validateResumedDispatch(root, entry, originalText, plan, outcome, session, timeout); err != nil {
		if durable {
			return ResumeLaunch{}, err
		}
		var cleanup error
		if cErr := cleanupFailedResume(plan, outcome); cErr != nil {
			cleanup = cErr
		}
		rollback := window.RestoreWindowText(root, entry, originalText)
		if msg := window.ResumeFailureMessage(err, cleanup, rollback); msg != "" {
			return ResumeLaunch{}, &Error{Message: msg}
		}
		return ResumeLaunch{}, err
	}
	return ResumeLaunch{Plan: plan, Outcome: outcome}, nil
}

func errorString(detail string) error {
	if detail == "" {
		return nil
	}
	return &Error{Message: detail}
}
