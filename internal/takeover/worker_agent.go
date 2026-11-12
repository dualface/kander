package takeover

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/worker"
)

func init() { worker.StopAgent = stopWorkerAgent }

// stopWorkerAgent uses only the immutable host-local address and session. It
// never reverse looks up another pane and never closes a terminal container.
func stopWorkerAgent(ctx context.Context, window, sessionValue string) error {
	backend, address, ok := terminal.ParseWindow(window)
	if !ok || !backend.Capabilities().Container {
		return errors.New("lost execution has no local terminal container")
	}
	agent, reference, _ := strings.Cut(sessionValue, " ")
	if agent == "" || reference == "" {
		return errors.New("lost execution has no complete agent session")
	}
	program, err := lookPath(backend.Executable())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return stopWorkerPane(ctx, backend, program, address, launch.AgentSession{Agent: agent, Reference: reference})
}
func sameWorkerPane(ctx context.Context, backend terminal.Backend, pane terminal.PaneFacts, session launch.AgentSession) error {
	if backend.Capabilities().AgentIdentity {
		if pane.MatchAgent(session.Agent) != terminal.AgentMatches {
			return errors.New("pane no longer proves the lost execution agent")
		}
		verdict, detail := terminal.MatchAgentSession(ctx, session.Agent, pane.AgentSessionKind, pane.AgentSession, session.Reference)
		if verdict != terminal.SessionMatches {
			return errors.New("pane no longer proves the lost execution session: " + detail)
		}
		return nil
	}
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return err
	}
	if pane.Dead != "0" || pane.InMode != "0" || pane.Command != expected || pane.SessionMarker != session.Reference {
		return errors.New("pane process or session differs from the lost execution")
	}
	return nil
}
func stopWorkerPane(ctx context.Context, backend terminal.Backend, program string, address terminal.Address, session launch.AgentSession) error {
	conn := probeConn(program)
	pane, err := backend.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return err
	}
	if pane.Gone {
		return nil
	}
	if pane.Container != "" && pane.Container != address.Container {
		return errors.New("lost execution pane moved to another container")
	}
	if err := sameWorkerPane(ctx, backend, pane, session); err != nil {
		return err
	}
	stopped := false
	if inspector, ok := backend.(terminal.ProcessInspector); ok {
		observed, inspectErr := inspector.ProcessFacts(ctx, conn, address.Pane)
		if inspectErr == nil {
			expected, err := agentCommandName(session.Agent)
			if err != nil {
				return err
			}
			var target *process.ProcessFacts
			for _, candidate := range observed {
				if candidate.Name != expected {
					continue
				}
				facts, err := process.ObserveProcess(ctx, candidate.PID)
				if err != nil || facts.Executable != expected {
					continue
				}
				if target != nil {
					return errors.New("pane has several matching agent processes")
				}
				target = &facts
			}
			if target != nil {
				// Recheck the pane after process collection. The signal itself uses a
				// pinned process handle, so PID reuse cannot redirect it.
				current, err := backend.PaneFacts(ctx, conn, address.Pane)
				if err != nil {
					return err
				}
				if err := sameWorkerPane(ctx, backend, current, session); err != nil {
					return err
				}
				boot, err := process.BootIdentity()
				if err != nil {
					return err
				}
				err = process.TerminateObserved(ctx, *target, boot)
				if err == nil {
					stopped = true
				} else if !errors.Is(err, process.ErrProcessUnsupported) {
					return err
				}
			}
		} else if !errors.Is(inspectErr, terminal.ErrUnsupported) {
			return inspectErr
		}
	}
	if !stopped {
		command, err := AgentExitCommand(session.Agent)
		if err != nil {
			return err
		}
		if err := backend.DeliverText(ctx, conn, address.Pane, command); err != nil {
			return err
		}
	}
	// Exit observation does not call the container cleanup routines.
	if backend.Capabilities().AgentIdentity {
		_, err = agentWaitExit(backend, program, address, session, 10)
	} else {
		_, err = processWaitExit(backend, program, address, session, 10)
	}
	return err
}
