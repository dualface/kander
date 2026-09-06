package notify

import (
	"fmt"
	"os"
	"runtime"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/window"
)

var isWindows = func() bool { return runtime.GOOS == "windows" }

func commandNotify(root, taskID, message, messageFile, pane string, messageSet bool, timeout float64) error {
	loaded, err := board.LoadBoard(root)
	if err != nil {
		return err
	}
	entry, err := board.Locate(loaded, taskID)
	if err != nil {
		return err
	}
	if entry.State != "review" && entry.State != "working" {
		return notifyError(
			"notify.only_tasks_in_review_or_working_can_be_notified", entry.TaskID, entry.State,
		)
	}
	if err := launch.ValidateTimeout(timeout, "notify"); err != nil {
		return err
	}
	msg, err := launch.ReadMessage(message, messageSet, messageFile, "notify")
	if err != nil {
		return err
	}
	text, err := board.ReadDocument(entry)
	if err != nil {
		return err
	}
	cfg, err := config.Load(true)
	if err != nil {
		return err
	}
	if err := cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(text)); err != nil {
		return err
	}
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		return err
	}
	// notify never moves the card state: a review card is moved by the notified execution agent itself,
	// the message body carries that requirement up front, and the review -> working transition is the acknowledgement of work starting.
	directMessage := launch.RuleLoadingInstruction(paths)
	if selfMove := launch.SelfMoveInstruction(paths, entry.TaskID, entry.State); selfMove != "" {
		directMessage += "\n\n" + selfMove
	}
	directMessage += "\n\n" + msg
	windowValue := board.MetadataFrom(text, window.WindowField)
	directError := t("notify.task_has_no_metadata")
	directChannel := ""
	acknowledgement := "N/A"
	acknowledgementDetail := ""
	messagePath := ""
	windowWritten := false
	resumed := false
	var resumeLaunch launch.ResumeLaunch

	err = func() error {
		session, err := launch.ResolvedSession(entry.TaskID, text)
		if err != nil {
			return err
		}
		liveSession := liveness.TaskSession{Agent: session.Agent, Reference: session.Reference}
		target, err := ResolveTarget(windowValue, pane, liveSession, timeout)
		if err != nil {
			return err
		}
		if isWindows() {
			return notifyError("notify.windows_currently_has_no_terminal_direct_delivery_channel")
		}
		if target.Window != "" && target.Window != windowValue {
			updated, err := window.RenderWindowMetadata(text, target.Window)
			if err != nil {
				return err
			}
			if err := window.WriteDocument(root, entry, updated); err != nil {
				return err
			}
			windowWritten = true
		}
		marker := ackMarker()
		path, err := writeNotifyMessage(directMessage)
		if err != nil {
			return err
		}
		messagePath = path
		instruction := notifyInstruction(entry, messagePath, marker)
		var confirmed bool
		if target.Kind == "herdr" {
			confirmed, acknowledgementDetail, err = herdrDirectNotify(target.Program, target.PaneID, instruction, marker, target.Timeout)
			directChannel = "herdr-direct"
		} else {
			confirmed, acknowledgementDetail, err = tmuxDirectNotify(target.Program, target.PaneID, instruction, marker, target.Timeout)
			directChannel = "tmux-direct"
		}
		if err != nil {
			directChannel = ""
			return err
		}
		if confirmed {
			acknowledgement = t("notify.confirmed")
		} else {
			acknowledgement = t("notify.unconfirmed")
		}
		return nil
	}()
	if err != nil {
		if isBusy(err) {
			return err
		}
		directError = err.Error()
		directChannel = ""
		if windowWritten {
			if rollback := window.RestoreWindowText(root, entry, text); rollback != nil {
				return notifyError(
					"notify.notify_direct_delivery_and_window_rollback_failed_direct_rollback", directError, rollback.Error(),
				)
			}
		}
		if messagePath != "" {
			if cleanup := removeNotifyMessage(messagePath); cleanup != nil {
				return notifyError(
					"notify.notify_direct_delivery_failed_and_message_cleanup_failed_direct", directError, cleanup.Error(),
				)
			}
			messagePath = ""
		}
		resumeLaunch, err = launch.NotifyViaResume(root, entry, text, msg, timeout)
		if err != nil {
			return notifyError(
				"notify.notify_failed_direct_resume", directError, err.Error(),
			)
		}
		resumed = true
	}

	channel := directChannel
	if resumed {
		channel = "resume"
	} else if acknowledgementDetail != "" {
		fmt.Fprintln(os.Stderr, t(
			"notify.warning_delivered_but_not_confirmed_within_the_timeout", acknowledgementDetail,
		))
	}

	outputPath := "N/A"
	channelDetail := ""
	if channel == "resume" {
		channelDetail = t("notify.direct_reason", directError)
	} else {
		outputPath = messagePath
	}
	fmt.Println(t(
		"notify.notified_channel_acknowledgement_message_file", entry.TaskID, channel, acknowledgement, outputPath, channelDetail,
	))
	flushStdout()
	if channel == "resume" && resumeLaunch.Plan.Launcher == "foreground" && resumeLaunch.Outcome.Wait != nil {
		code, waitErr := resumeLaunch.Outcome.Wait()
		if waitErr != nil {
			return waitErr
		}
		if code != 0 {
			return notifyError(
				"notify.resumed_agent_passed_liveness_validation_then_exited_with_status", code,
			)
		}
	}
	return nil
}
