package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/window"
)

func commandNotify(root, task, message, messageFile, pane string, messageSet bool, timeout float64, options ...launch.DispatchOptions) error {
	if err := launch.ValidateTimeout(timeout, "notify"); err != nil {
		return err
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		return err
	}
	task = s.Entry.TaskID
	msg, err := launch.ReadMessage(message, messageSet, messageFile, "notify")
	if err != nil {
		return err
	}
	cfg, err := config.Load(false)
	if err != nil {
		return err
	}
	if err = cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(s.Text)); err != nil {
		return err
	}
	var o launch.DispatchOptions
	if len(options) > 0 {
		o = options[0]
	}
	d, active, err := launch.PrepareAction(root, s, msg, o, timeout)
	if err != nil {
		return err
	}
	if !active {
		return commandNotifyLegacy(root, task, message, messageFile, pane, messageSet, timeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout*float64(time.Second)))
	defer cancel()
	var resumed launch.ResumeLaunch
	err = board.WithDispatchDelivery(root, d.Input.ID, func() error { return deliverDispatchContext(ctx, root, task, d.Input.ID, pane, &resumed) })
	if err != nil {
		return err
	}
	return launch.WaitForResumedForeground(resumed)
}

func receiptExists(d board.Dispatch) bool {
	return d.State == board.DispatchAccepted || d.State == board.DispatchCompleted
}

func deliverDispatch(root, task, id, paneOverride string, resumed ...*launch.ResumeLaunch) (deliveryErr error) {
	return deliverDispatchContext(context.Background(), root, task, id, paneOverride, resumed...)
}

func deliverDispatchContext(parent context.Context, root, task, id, paneOverride string, resumed ...*launch.ResumeLaunch) (deliveryErr error) {
	d, err := board.ReadDispatch(root, task, id)
	if err != nil {
		return err
	}
	authorization := d.Authorization
	defer func() {
		if deliveryErr == nil {
			return
		}
		current, err := board.ReadDispatch(root, task, id)
		if err == nil && current.Authorization == authorization && receiptExists(current) {
			deliveryErr = launch.PrintDispatchResult(current)
		}
	}()
	if receiptExists(d) {
		return launch.PrintDispatchResult(d)
	}
	if d.State != board.DispatchPrepared && d.State != board.DispatchUnknown {
		return notifyError("launch.dispatch_pending", id)
	}
	ctx, cancel := context.WithDeadline(parent, d.Input.ConfirmBy)
	defer cancel()
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		return err
	}
	s, err := board.ReadExecutionSnapshot(root, task, authorization)
	if err != nil {
		return err
	}
	rules, err := launch.RuleLoadingWithLanguage(paths, s.Text)
	if err != nil {
		return err
	}
	message := rules + "\n\n" + launch.DispatchInstruction(paths, d) + "\n\n" + d.Input.Message
	for {
		if ctx.Err() != nil {
			return notifyError("launch.dispatch_pending", id)
		}
		// Receipt reconciliation precedes every observation and retry.
		d, err = board.ReadDispatch(root, task, id)
		if err != nil {
			return err
		}
		if d.Authorization != authorization {
			return notifyError("board.dispatch_conflict", id)
		}
		if receiptExists(d) {
			return launch.PrintDispatchResult(d)
		}
		s, err = board.ReadExecutionSnapshot(root, task, authorization)
		if err != nil {
			return err
		}
		if err = launch.ValidateActionEvidence(ctx, root, d); err != nil {
			return err
		}
		target, stopped, busy, err := resolveDispatchTarget(ctx, s, paneOverride)
		if err != nil {
			return err
		}
		if stopped {
			d, err = board.BeginDispatchAttempt(root, task, id, d.Revision, s.Revision)
			if err != nil {
				return reconcileDispatch(root, task, id, err)
			}
			s, err = board.ReadExecutionSnapshot(root, task, d.Authorization)
			if err != nil {
				return err
			}
			deadline, _ := ctx.Deadline()
			result, resumeErr := launch.NotifyViaResume(root, s.Entry, s.Text, launch.DispatchInstruction(paths, d)+"\n\n"+d.Input.Message, time.Until(deadline).Seconds())
			if len(resumed) > 0 {
				*resumed[0] = result
			}
			err = resumeErr
			if err != nil {
				return reconcileDispatch(root, task, id, err)
			}
			return awaitDispatch(ctx, root, task, id)
		}
		if busy {
			select {
			case <-ctx.Done():
				return &BusyError{Message: t("notify.target_agent_is_busy_nothing_was_delivered", id)}
			case <-time.After(pollInterval):
				continue
			}
		}
		if target.Window != "" && target.Window != board.MetadataFrom(s.Text, board.FieldWindow) {
			text, err := window.RenderWindowMetadata(s.Text, target.Window)
			if err != nil {
				return err
			}
			if err = window.WriteDocument(root, s.Entry, text); err != nil {
				return err
			}
			s.Revision++
		}
		path, err := writeNotifyMessage(message)
		if err != nil {
			return err
		}
		// A persisted unknown precedes the actual send. Retain payload after this
		// point: a transport error may still have delivered its pathname.
		d, err = board.BeginDispatchAttempt(root, task, id, d.Revision, s.Revision)
		if err != nil {
			cleanup := removeNotifyMessage(path)
			if cleanup != nil {
				return cleanup
			}
			return reconcileDispatch(root, task, id, err)
		}
		marker := ackMarker()
		instruction := notifyInstruction(s.Entry, path, marker)
		if err = sendDispatch(ctx, target, instruction); err != nil {
			return reconcileDispatch(root, task, id, err)
		}
		return awaitDispatch(ctx, root, task, id)
	}
}

func reconcileDispatch(root, task, id string, sendErr error) error {
	d, err := board.ReadDispatch(root, task, id)
	if err != nil {
		return err
	}
	if receiptExists(d) {
		return launch.PrintDispatchResult(d)
	}
	if err = launch.PrintDispatchResult(d); err != nil {
		return err
	}
	return sendErr
}
func awaitDispatch(ctx context.Context, root, task, id string) error {
	for {
		d, err := board.ReadDispatch(root, task, id)
		if err != nil {
			return err
		}
		if receiptExists(d) {
			return launch.PrintDispatchResult(d)
		}
		select {
		case <-ctx.Done():
			return reconcileDispatch(root, task, id, notifyError("launch.dispatch_pending", id))
		case <-time.After(pollInterval):
		}
	}
}

// dispatchTarget rechecks readiness with the same context as P3's observation.
// Missing identity and probe errors never authorize a recovery process.
func dispatchTarget(ctx context.Context, value, override, task, text string) (DirectTarget, bool, error) {
	session, err := launch.ResolvedSession(task, text)
	if err != nil {
		return DirectTarget{}, false, err
	}
	if match := herdrWindowRe.FindStringSubmatch(value); match != nil || override != "" {
		program, err := lookPath("herdr")
		if err != nil {
			return DirectTarget{}, false, err
		}
		pane := override
		if pane == "" {
			pane = match[2]
		}
		found, err := probe.ProbeHerdrPaneContext(ctx, program, pane)
		if err != nil {
			return DirectTarget{}, false, err
		}
		if found.Pane == nil || session.Reference == "" || herdrSessionReference(found.Pane) != session.Reference || found.Pane["agent"] != session.Agent {
			return DirectTarget{}, false, notifyError("launch.dispatch_recovery_unproven", task, "identity")
		}
		status, _ := found.Pane["agent_status"].(string)
		tab := probe.PublicID(found.Pane["tab_id"])
		if tab == "" {
			return DirectTarget{}, false, notifyError("launch.dispatch_recovery_unproven", task, "tab")
		}
		return DirectTarget{Kind: "herdr", Program: program, PaneID: pane, Window: "herdr:" + tab + ":" + pane}, status != "idle" && status != "done", nil
	}
	if match := tmuxWindowRe.FindStringSubmatch(value); match != nil {
		program, err := lookPath("tmux")
		if err != nil {
			return DirectTarget{}, false, err
		}
		found, err := probe.ProbeTmuxPaneContext(ctx, program, match[4])
		if err != nil {
			return DirectTarget{}, false, err
		}
		f := found.Facts
		expected, err := agentCommandName(session.Agent)
		if err != nil {
			return DirectTarget{}, false, err
		}
		if f == nil || f.Dead != "0" || f.Command != expected || session.Reference == "" || f.SessionMarker != session.Reference {
			return DirectTarget{}, false, notifyError("launch.dispatch_recovery_unproven", task, "identity")
		}
		return DirectTarget{Kind: "tmux", Program: program, PaneID: match[4], Window: value}, f.InMode != "0", nil
	}
	return DirectTarget{}, false, notifyError("launch.dispatch_recovery_unproven", task, value)
}

func sendDispatch(ctx context.Context, target DirectTarget, instruction string) error {
	var commands [][]string
	if target.Kind == "herdr" {
		commands = [][]string{{"agent", "prompt", target.PaneID, instruction}}
	} else {
		commands = [][]string{{"send-keys", "-t", target.PaneID, "-l", instruction}, {"send-keys", "-t", target.PaneID, "Enter"}}
	}
	for _, args := range commands {
		result, e := probe.CaptureContext(ctx, target.Program, args)
		if e != nil {
			return e
		}
		if result.Code != 0 {
			return fmt.Errorf("%s", herdrFailureDetail(result))
		}
	}
	return nil
}

// One P3 budget covers forward/reverse observation and final readiness checks.
func resolveDispatchTarget(ctx context.Context, s board.Snapshot, override string) (DirectTarget, bool, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, liveness.DefaultBatchBudget)
	defer cancel()
	value := board.MetadataFrom(s.Text, board.FieldWindow)
	if override == "" {
		observation := launch.DispatchObservation(ctx, s)
		if !observation.ValidFor(s.Entry, s.Text) {
			return DirectTarget{}, false, false, notifyError("launch.dispatch_recovery_unproven", s.Entry.TaskID, observation.Detail)
		}
		if observation.Status == liveness.Stopped {
			return DirectTarget{}, true, false, nil
		}
		if observation.Status != liveness.Alive && observation.Status != liveness.Drifted {
			return DirectTarget{}, false, false, notifyError("launch.dispatch_recovery_unproven", s.Entry.TaskID, observation.Detail)
		}
		if observation.NewWindow != "" {
			value = observation.NewWindow
		}
	}
	target, busy, err := dispatchTarget(ctx, value, override, s.Entry.TaskID, s.Text)
	return target, false, busy, err
}
