package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
)

// DispatchOptions opt working/generic messages into the durable protocol.
// Group review dispatches opt in automatically, defaulting to fix.
type DispatchOptions struct{ ID, Kind, Base, EvidenceFile string }

// PrepareAction resolves defaults once; a retry compares the original payload
// and never derives a new baseline or confirmation deadline.
func PrepareAction(root string, s board.Snapshot, message string, o DispatchOptions, timeout float64) (board.Dispatch, bool, error) {
	active := o.EvidenceFile != "" || o.ID != "" || o.Kind != "" || o.Base != "" || s.Entry.State == "review" && (board.TaskGroupFrom(s.Text) != "" || board.MetadataFrom(s.Text, "DISPATCH_ID") != "")
	if !active {
		return board.Dispatch{}, false, nil
	}
	evidence, err := readDispatchEvidence(o.EvidenceFile)
	if err != nil {
		return board.Dispatch{}, true, err
	}
	if o.ID != "" {
		previous, err := board.ReadDispatch(root, s.Entry.TaskID, o.ID)
		if err == nil {
			if o.Kind == "" {
				o.Kind = previous.Input.Kind
			}
			if o.Base == "" {
				o.Base = previous.Input.Base
			}
			in := previous.Input
			in.Message = message
			in.Kind = o.Kind
			in.Base = o.Base
			if o.EvidenceFile != "" {
				in.Evidence = evidence
			}
			d, e := PrepareBoundDispatch(root, in)
			return d, true, e
		}
		if !errors.Is(err, os.ErrNotExist) {
			return board.Dispatch{}, true, err
		}
	}
	if o.Kind == "" {
		o.Kind = "fix"
	}
	if o.ID == "" {
		var err error
		o.ID, err = board.NewDispatchID()
		if err != nil {
			return board.Dispatch{}, true, err
		}
	}
	// Print identity even if later preparation fails, and always before a send.
	fmt.Fprintln(os.Stdout, t("launch.dispatch_id", o.ID))
	if o.Base == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
		if err != nil {
			return board.Dispatch{}, true, err
		}
		o.Base = strings.TrimSpace(string(b))
	}
	in := board.DispatchInput{ID: o.ID, TaskID: s.Entry.TaskID, Kind: o.Kind, Message: message, Base: o.Base, Evidence: evidence}
	// Defaults are persisted by the board; only this new request supplies a deadline.
	in.CreatedAt = time.Now().UTC()
	in.ConfirmBy = in.CreatedAt.Add(time.Duration(timeout * float64(time.Second)))
	d, err := PrepareBoundDispatch(root, in)
	return d, true, err
}

// DispatchInstruction names the only business acknowledgement and completion
// entrances. A repeated receipt must never start the external work twice.
func DispatchInstruction(paths config.InstallPaths, d board.Dispatch) string {
	return t("launch.dispatch_prompt", commandName(paths), d.Input.TaskID, d.Input.ID, d.Authorization.Epoch, d.Input.Kind) + "\n" + board.DispatchEvidenceInstruction(d)
}

// DispatchObservation uses P3's shared deadline and identity-valid report. A
// transport recovery is permitted only from a valid stopped observation.
func DispatchObservation(ctx context.Context, s board.Snapshot) liveness.Report {
	reports := liveness.ClassifyTasksContext(ctx, []liveness.TaskInput{{Entry: s.Entry, Text: s.Text}}, liveness.BatchOptions{})
	return reports[0]
}

func dispatchFinished(d board.Dispatch) bool {
	return d.State == board.DispatchAccepted || d.State == board.DispatchCompleted
}

// PrintDispatchResult reports durable state, never terminal prompt echo.
func PrintDispatchResult(d board.Dispatch) error { return json.NewEncoder(os.Stdout).Encode(d) }

func commandResume(root string, agent *string, launcher, task, message, messageFile string, messageSet bool, timeout float64, options ...DispatchOptions) error {
	if err := ValidateTimeout(timeout, "resume"); err != nil {
		return err
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		return err
	}
	task = s.Entry.TaskID
	msg, err := ReadMessage(message, messageSet, messageFile, "resume")
	if err != nil {
		return err
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	if err = cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(s.Text)); err != nil {
		return err
	}
	var o DispatchOptions
	if len(options) > 0 {
		o = options[0]
	}
	d, active, err := PrepareAction(root, s, msg, o, timeout)
	if err != nil {
		return err
	}
	if !active {
		if board.MetadataFrom(s.Text, "DISPATCH_ID") != "" {
			return launchError("board.dispatch_conflict", board.MetadataFrom(s.Text, "DISPATCH_ID"))
		}
		return commandResumeLegacy(root, agent, launcher, task, message, messageFile, messageSet, timeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout*float64(time.Second)))
	defer cancel()
	var launched ResumeLaunch
	err = board.WithDispatchDelivery(root, d.Input.ID, func() error {
		current, err := board.ReadDispatch(root, task, d.Input.ID)
		if err != nil {
			return err
		}
		err = resumeDispatch(ctx, root, agent, launcher, task, current, &launched)
		if err != nil {
			latest, readErr := board.ReadDispatch(root, task, d.Input.ID)
			if readErr != nil {
				return readErr
			}
			if dispatchFinished(latest) && (agent == nil || latest.Authorization.Epoch > current.Authorization.Epoch) {
				return PrintDispatchResult(latest)
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	return WaitForResumedForeground(launched)
}

func resumeDispatch(parent context.Context, root string, agent *string, launcher, task string, d board.Dispatch, launched *ResumeLaunch) error {
	if dispatchFinished(d) && agent == nil {
		return PrintDispatchResult(d)
	}
	ctx, cancel := context.WithDeadline(parent, d.Input.ConfirmBy)
	defer cancel()
	if err := ValidateActionEvidence(ctx, root, d); err != nil {
		return err
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		return err
	}
	if agent == nil {
		observation := DispatchObservation(ctx, s)
		if !observation.ValidFor(s.Entry, s.Text) || observation.Status != liveness.Stopped {
			return launchError("launch.dispatch_recovery_unproven", d.Input.ID, observation.Detail)
		}
	} else {
		// An explicit --agent is the existing user-authorized takeover entrance.
		d, err = board.ReauthorizeDispatch(root, task, d.Input.ID, d.Revision)
		if err != nil {
			return err
		}
		s, err = board.ReadExecutionSnapshot(root, task, d.Authorization)
		if err != nil {
			return err
		}
	}
	d, err = board.BeginDispatchAttempt(root, task, d.Input.ID, d.Revision, s.Revision)
	if err != nil {
		return err
	}
	// Legacy launch now operates on the current fenced snapshot; its rollback
	// cannot erase a receipt consumed while launch/liveness validation ran.
	paths, err := currentInstallPaths()
	if err != nil {
		return err
	}
	msg := DispatchInstruction(paths, d) + "\n\n" + d.Input.Message
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Seconds()
	if remaining <= 0 {
		return launchError("launch.dispatch_pending", d.Input.ID)
	}
	err = commandResumeLegacy(root, agent, launcher, task, msg, "", true, remaining, resumeBinding{Authorization: d.Authorization, Outcome: launched})
	current, readErr := board.ReadDispatch(root, task, d.Input.ID)
	if readErr != nil {
		return readErr
	}
	if dispatchFinished(current) {
		return PrintDispatchResult(current)
	}
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return launchError("launch.dispatch_pending", d.Input.ID)
	}
	return PrintDispatchResult(current)
}

// ParseDispatchOptions shares the notify/resume flag spelling and rejects duplicates.
func ParseDispatchOptions(args []string) ([]string, DispatchOptions, error) {
	var o DispatchOptions
	var rest []string
	values := map[string]*string{"--dispatch-id": &o.ID, "--kind": &o.Kind, "--base": &o.Base, "--evidence-file": &o.EvidenceFile}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		field, ok := values[name]
		if !ok {
			rest = append(rest, args[i])
			// Preserve the value boundary owned by the notify/resume parser.
			// A message or path may itself look like a dispatch control option.
			if !inline && i+1 < len(args) {
				switch name {
				case "--message", "--message-file", "--timeout", "--agent", "--launcher", "--pane":
					i++
					rest = append(rest, args[i])
				}
			}
			continue
		}
		if seen[name] {
			return nil, o, launchError("board.unknown_option", name)
		}
		seen[name] = true
		if !inline {
			i++
			if i >= len(args) {
				return nil, o, launchError("board.option_requires_a_value", name)
			}
			value = args[i]
		}
		if value == "" {
			return nil, o, launchError("board.option_requires_a_value", name)
		}
		*field = value
	}
	return rest, o, nil
}

func validateResumedDispatch(root string, entry board.Entry, text string, plan LaunchPlan, outcome LaunchOutcome, session AgentSession, timeout float64) error {
	id := board.MetadataFrom(text, "DISPATCH_ID")
	if id == "" {
		return validateResumedAgent(plan, outcome, session, timeout)
	}
	d, err := board.ReadDispatch(root, entry.TaskID, id)
	if err != nil {
		return err
	}
	remaining := time.Until(d.Input.ConfirmBy).Seconds()
	if remaining < timeout {
		timeout = remaining
	}
	receipt := func() (bool, error) {
		current, err := board.ReadDispatch(root, entry.TaskID, id)
		if err != nil {
			return false, err
		}
		if current.Authorization != d.Authorization {
			return false, launchError("board.dispatch_conflict", id)
		}
		return dispatchFinished(current), nil
	}
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	if err = validateResumedAgentReceipt(plan, outcome, session, timeout, receipt); err != nil {
		return err
	}
	// Process liveness can consume the entire acceptance budget. Reconcile once
	// more before reporting pending; preserve any executor whose delivery is unknown.
	consumed, err := receipt()
	if err != nil {
		return err
	}
	if !consumed && !nowFn().Before(deadline) {
		return launchError("launch.dispatch_pending", id)
	}
	return nil
}

type resumeBinding struct {
	Authorization board.ExecutionAuthorization
	Outcome       *ResumeLaunch
}

// WaitForResumedForeground preserves foreground ownership after the bounded
// dispatch phase. Call only after releasing the transport lease.
func WaitForResumedForeground(result ResumeLaunch) error {
	if result.Plan.Launcher != "foreground" || result.Outcome.Wait == nil {
		return nil
	}
	code, err := result.Outcome.Wait()
	if err != nil {
		return err
	}
	if code != 0 {
		return launchError("notify.resumed_agent_passed_liveness_validation_then_exited_with_status", code)
	}
	return nil
}
