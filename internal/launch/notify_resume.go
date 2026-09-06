package launch

import (
	"github.com/dualface/kander/internal/board"
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

// ResolvedSession resolves the card session; Codex cards without an id go through rollout lookup.
func ResolvedSession(taskID, text string) (AgentSession, error) {
	return resolvedTaskSession(taskID, text)
}

// NotifyViaResume resumes the original session after direct delivery failed; on failure it restores the card text as it was before the call.
func NotifyViaResume(root string, entry board.Entry, originalText, message string, timeout float64) (ResumeLaunch, error) {
	session, err := resolvedTaskSession(entry.TaskID, originalText)
	if err != nil {
		return ResumeLaunch{}, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return ResumeLaunch{}, err
	}
	if err := cfg.Rules.CheckTaskGroup(taskGroupFrom(originalText)); err != nil {
		return ResumeLaunch{}, err
	}
	plan, err := prepareLaunch(cfg.Launcher, parentDir(root), "notify")
	if err != nil {
		return ResumeLaunch{}, err
	}
	program, err := requireAgentProgram(session.Agent)
	if err != nil {
		return ResumeLaunch{}, err
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return ResumeLaunch{}, err
	}
	promptBody, err := resumePrompt(entry.TaskID, message, paths, entry.State)
	if err != nil {
		return ResumeLaunch{}, err
	}
	taskFile, err := createTaskFile(promptBody, "kander-"+entry.TaskID+"-notify-")
	if err != nil {
		return ResumeLaunch{}, err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.resume_head", entry.TaskID), taskFile)
	model := cfg.Models.Kanban[session.Agent]
	args, err := agentArguments(session.Agent, model, entry.Kind, session, true)
	if err != nil {
		return ResumeLaunch{}, err
	}
	inv, err := launchInvocation(plan, *program, append(args, prompt))
	if err != nil {
		return ResumeLaunch{}, err
	}
	if plan.Launcher == "foreground" || plan.Launcher == "console" {
		current, err := readDocumentFn(entry)
		if err != nil {
			return ResumeLaunch{}, err
		}
		updated, err := windowMetadata(current, plan.Launcher)
		if err != nil {
			return ResumeLaunch{}, err
		}
		if err := writeDocumentFn(root, entry, updated); err != nil {
			return ResumeLaunch{}, err
		}
	}
	paneCB := (func() (AgentSession, error))(nil)
	if plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		paneCB = func() (AgentSession, error) { return session, nil }
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.Launcher == "herdr" || plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		loc = recordWindowLocation(root, plan, entry)
	}
	outcome, err := launchAgent(plan, root, windowName(entry, originalText), inv, loc, paneCB, &session)
	if err != nil {
		failure := asLaunchFailure(err)
		rollback := window.RestoreWindowText(root, entry, originalText)
		if msg := window.ResumeFailureMessage(failure.Err, errorString(failure.CloseError), rollback); msg != "" {
			return ResumeLaunch{}, &Error{Message: msg}
		}
		return ResumeLaunch{}, err
	}
	taskFileHandedOff = true
	if err := validateResumedAgent(plan, outcome, session, timeout); err != nil {
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
