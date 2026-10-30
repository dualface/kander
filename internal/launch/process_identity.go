package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

var observeSettledFn = process.ObserveSettled

// processRecorder returns the step that records the identity of a process a
// launcher without a pane started. A failure only warns: the executor runs, and
// the liveness check reads the missing identity as unknown with its reason.
func processRecorder(root string, entry board.Entry, plan LaunchPlan) func(int) {
	if plan.capabilities().Container {
		return nil
	}
	warn := plan.warning
	if warn == nil {
		warn = func(message string) { fmt.Fprint(os.Stderr, message) }
	}
	return func(pid int) {
		identity, err := observeIdentity(context.Background(), plan, pid)
		if err == nil {
			_, err = board.RecordExecutionProcess(root, entry, identity)
		}
		if err != nil && !errors.Is(err, board.ErrExecutionSuperseded) {
			warn(t("launch.process_identity_not_recorded", entry.TaskID, err.Error()))
		}
	}
}

// observeIdentity reads the identity from the kernel, so the check later compares
// like with like; the configured agent name may be a shim. On a platform without
// process identity only the launcher and the ID are recorded, and the check says
// unprobeable.
func observeIdentity(ctx context.Context, plan LaunchPlan, pid int) (board.ExecutionProcess, error) {
	identity := board.ExecutionProcess{Launcher: plan.Launcher, AgentPID: pid, RecordedAt: nowFn().UTC().Format(time.RFC3339)}
	agent, err := observeSettledFn(ctx, pid)
	if errors.Is(err, process.ErrProcessUnsupported) {
		return identity, nil
	}
	if err != nil {
		return identity, err
	}
	identity.AgentStart, identity.Executable = agent.Start, agent.Executable
	if identity.BootID, err = process.BootIdentity(); err != nil {
		return identity, err
	}
	if plan.OccupiesTerminal() {
		starter, err := process.ObserveProcess(ctx, os.Getpid())
		if err != nil {
			return identity, err
		}
		identity.KanderPID, identity.KanderStart = starter.PID, starter.Start
	}
	return identity, nil
}

// refuseNativeResume keeps the boundary of foreground and console: a session there
// has no native resume. A relaunch on the same record would leave the immutable
// identity naming the old process, and beside a live executor it would be a second
// one. A card without an identity comes from an old binary and keeps its behavior.
func refuseNativeResume(task, text string, record board.ExecutionRecord, plan LaunchPlan) error {
	window := metadataFrom(text, windowField)
	direct := terminal.HasCapability(window, func(c terminal.Capabilities) bool { return !c.Container })
	if record.Process == nil || !direct && plan.capabilities().Container {
		return nil
	}
	return launchError("launch.native_resume_unsupported", task, orNA(window))
}
