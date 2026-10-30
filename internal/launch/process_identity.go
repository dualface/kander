package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

var observeSettledFn = process.ObserveSettled

// processRecorder returns the step that records the identity of a process a
// launcher without a pane started. The identity is immutable, so it is written only
// once the executable is final. The kernel image of a shim says nothing about the
// exec still to come, and no signal announces it, so a shim waits for the first
// executor evidence in the background: only a foreground Kander stays to do that.
// Until then the check says unknown, never a mismatch for a healthy process. The
// returned channel closes when the step ended.
func processRecorder(root string, entry board.Entry, plan LaunchPlan) func(pid int, running func() bool, program string) <-chan struct{} {
	warn := plan.warning
	if warn == nil {
		warn = func(message string) { fmt.Fprint(os.Stderr, message) }
	}
	observed := observeSettledFn
	ready := func() bool {
		standing, err := board.ReadExecutionStanding(root, entry)
		return err == nil && standing.Evidence
	}
	publish := func(identity board.ExecutionProcess) error {
		_, err := board.RecordExecutionProcess(root, entry, identity)
		return err
	}
	return func(pid int, running func() bool, program string) <-chan struct{} {
		done := make(chan struct{})
		ctx := context.Background()
		agent, err := observed(ctx, pid, running)
		final := errors.Is(err, process.ErrProcessUnsupported) || err == nil && process.ImageConfirmed(resolvedProgram(program), agent)
		if err == nil && !final && plan.OccupiesTerminal() {
			go func() {
				defer close(done)
				// A failure stays silent: the terminal belongs to the agent, and
				// the check names the missing identity.
				if agent, err := process.ObserveWhenReady(ctx, agent, running, ready); err == nil {
					if identity, err := identityOf(ctx, plan, pid, agent); err == nil {
						_ = publish(identity)
					}
				}
			}()
			return done
		}
		defer close(done)
		switch {
		case err == nil && !final:
			err = process.ErrProcessUnsettled
		case err == nil:
			var identity board.ExecutionProcess
			if identity, err = identityOf(ctx, plan, pid, agent); err == nil {
				err = publish(identity)
			}
		case errors.Is(err, process.ErrProcessUnsupported):
			err = publish(partialIdentity(plan, pid))
		}
		if err != nil && !errors.Is(err, board.ErrExecutionSuperseded) {
			warn(t("launch.process_identity_not_recorded", entry.TaskID, err.Error()))
		}
		return done
	}
}

// partialIdentity is what a platform without process identity records, so the check
// says unprobeable.
func partialIdentity(plan LaunchPlan, pid int) board.ExecutionProcess {
	return board.ExecutionProcess{Launcher: plan.Launcher, AgentPID: pid, RecordedAt: time.Now().UTC().Format(time.RFC3339)}
}

// identityOf adds what the kernel reports, so the check later compares like with
// like; the configured agent name may be a shim.
func identityOf(ctx context.Context, plan LaunchPlan, pid int, agent process.ProcessFacts) (board.ExecutionProcess, error) {
	identity := partialIdentity(plan, pid)
	identity.AgentStart, identity.Executable = agent.Start, agent.Executable
	var err error
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

// resolvedProgram follows PATH and symlinks to the file the kernel runs.
func resolvedProgram(program string) string {
	if found, err := lookPathExec(program); err == nil {
		program = found
	}
	if real, err := filepath.EvalSymlinks(program); err == nil {
		return real
	}
	return program
}

// refuseNativeResume keeps the boundary of foreground and console: a session there
// has no native resume, whichever side of the relaunch has no pane. A relaunch would
// leave the immutable identity naming the old process, and beside a live executor it
// would be a second one. An explicit takeover opens a new record and is not refused.
func refuseNativeResume(task, text string, plan LaunchPlan) error {
	window := metadataFrom(text, windowField)
	direct := terminal.HasCapability(window, func(c terminal.Capabilities) bool { return !c.Container })
	if !direct && plan.capabilities().Container {
		return nil
	}
	return launchError("launch.native_resume_unsupported", task, orNA(window))
}
