package liveness

import (
	"context"
	"errors"
	"strconv"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal/direct"
)

// processProbe is the kernel side of the identity check; tests replace it.
var processProbe = struct {
	supported func() bool
	boot      func() (string, error)
	observe   func(context.Context, int) (process.ProcessFacts, error)
}{
	supported: func() bool { return process.ProcessIdentitySupported },
	boot:      process.BootIdentity,
	observe:   process.ObserveProcess,
}

// classifyProcess checks the identity recorded for a launcher without a pane. The
// first failed step decides, and only a complete match says alive. A process ID
// alone proves nothing: identity is the boot, the ID, the kernel start time and the
// executable together.
func classifyProcess(ctx context.Context, entry board.Entry, text string, session TaskSession, window string) Report {
	done := func(status, detail string) Report {
		return report(entry, &session, status, window, window, detail, "")
	}
	failed := func(err error) Report {
		if errors.Is(err, process.ErrProcessDenied) || errors.Is(err, process.ErrProcessUnsupported) {
			return done(Unprobeable, t("liveness.process_probe_denied", probe.FailureDetail(err)))
		}
		return done(Unknown, probe.FailureDetail(err))
	}
	view, err := board.ReadEntryExecution(ctx, entry)
	if err != nil {
		return failed(err)
	}
	if !view.Exists || view.Record.Process == nil {
		if view.Stored {
			return done(Unknown, t("liveness.process_identity_unrecorded"))
		}
		return done(Unknown, t("liveness.process_identity_legacy"))
	}
	recorded := *view.Record.Process
	if !processProbe.supported() {
		return done(Unprobeable, t("liveness.process_platform_unsupported"))
	}
	boot, err := processProbe.boot()
	if err != nil {
		return failed(err)
	}
	if !process.SameBoot(recorded.BootID, boot) {
		return done(Stopped, t("liveness.process_rebooted"))
	}
	agent, err := processProbe.observe(ctx, recorded.AgentPID)
	if errors.Is(err, process.ErrProcessGone) {
		return done(Stopped, t("liveness.process_exited", strconv.Itoa(recorded.AgentPID)))
	}
	if err != nil {
		return failed(err)
	}
	if agent.Start != recorded.AgentStart {
		return done(Stopped, t("liveness.process_pid_reused", strconv.Itoa(recorded.AgentPID)))
	}
	if agent.Executable != recorded.Executable {
		return done(IdentityMismatch, t("liveness.process_executable_mismatch", recorded.Executable, agent.Executable))
	}
	if view.Record.Owner != board.MetadataFrom(text, board.FieldOwner) || view.Record.Cycle != board.ExecutionCycle(entry, text) {
		return done(IdentityMismatch, t("liveness.process_card_transferred"))
	}
	alive := t("liveness.process_identity_matches", strconv.Itoa(recorded.AgentPID))
	if recorded.Launcher != direct.Foreground {
		return done(Alive, alive)
	}
	starter, err := processProbe.observe(ctx, recorded.KanderPID)
	switch {
	case errors.Is(err, process.ErrProcessGone):
		return done(AliveOrphaned, t("liveness.process_starter_gone", strconv.Itoa(recorded.KanderPID)))
	case err != nil:
		return failed(err)
	case starter.Start != recorded.KanderStart || agent.ParentPID != recorded.KanderPID:
		return done(AliveOrphaned, t("liveness.process_starter_gone", strconv.Itoa(recorded.KanderPID)))
	}
	return done(Alive, alive)
}
