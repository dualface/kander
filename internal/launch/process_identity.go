package launch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

// Test seams: imageCanChange stands for a platform without exec.
var (
	observeStartedFn = process.ObserveStarted
	imageCanChange   = process.ImageCanChange
)

// processRecorder returns the step that records the identity of a process a
// launcher without a pane started. The identity is immutable, so it is written only
// once the executable is final. A running process can still exec, nothing announces
// that, and a healthy agent must not read as a mismatch. The one fact only the agent
// produces is the read of its task file: a shim never opens it, and neither a card
// update nor a dispatch touches it. Until then the check says unknown. Only a
// foreground Kander stays to watch; console exists on Windows, which has no exec.
// The returned channel closes when the step ended.
func processRecorder(root string, entry board.Entry, plan LaunchPlan, taskFile string) func(pid int, running func() bool) <-chan struct{} {
	warn := plan.warning
	if warn == nil {
		warn = func(message string) { fmt.Fprint(os.Stderr, message) }
	}
	observed, watch := observeStartedFn, imageCanChange
	read := taskFileRead(taskFile)
	publish := func(identity board.ExecutionProcess) error {
		_, err := board.RecordExecutionProcess(root, entry, identity)
		return err
	}
	return func(pid int, running func() bool) <-chan struct{} {
		done := make(chan struct{})
		ctx := context.Background()
		agent, err := observed(ctx, pid, running)
		if err == nil && watch {
			go func() {
				defer close(done)
				// A failure stays silent: the terminal belongs to the agent, and
				// the check names the missing identity.
				if agent, err := process.ObserveWhenReady(ctx, agent, running, read); err == nil {
					if identity, err := identityOf(ctx, plan, pid, agent); err == nil {
						_ = publish(identity)
					}
				}
			}()
			return done
		}
		defer close(done)
		switch {
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

// taskFileRead reports a read of the task file since this call; the launcher calls
// it before the agent starts. The executor removes the file when it is done, which
// counts too. A mount without access times never reports, so the check stays unknown.
func taskFileRead(path string) func() bool {
	baseline, err := process.FileAccessTime(path)
	return func() bool {
		if err != nil {
			return false
		}
		current, e := process.FileAccessTime(path)
		return errors.Is(e, fs.ErrNotExist) || e == nil && current.After(baseline)
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
