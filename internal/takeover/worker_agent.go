package takeover

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/launch"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

func init() { worker.StopAgent = stopWorkerAgent }

// stopWorkerAgent uses only the immutable host-local address and session. It
// never reverse looks up another pane and never closes a terminal container.
// Every agent definition it reads, the exit command included, comes from the
// project overlay of the address's configuration directory.
func stopWorkerAgent(ctx context.Context, target worker.AgentAddress) error {
	backend, address, ok := terminal.ParseWindow(target.Window)
	if !ok || !backend.Capabilities().Container {
		return errors.New("lost execution has no local terminal container")
	}
	agent, reference, _ := strings.Cut(target.Session, " ")
	if agent == "" || reference == "" {
		return errors.New("lost execution has no complete agent session")
	}
	program, err := lookPath(backend.Executable())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return stopWorkerPane(ctx, backend, program, address, launch.AgentSession{Agent: agent, Reference: reference}, config.AgentLookupIn(target.ConfigDir))
}
func sameWorkerPane(ctx context.Context, backend terminal.Backend, pane terminal.PaneFacts, session launch.AgentSession, lookup config.AgentLookup) error {
	if backend.Capabilities().AgentIdentity {
		if pane.MatchAgentWith(session.Agent, lookup) != terminal.AgentMatches {
			return errors.New("pane no longer proves the lost execution agent")
		}
		verdict, detail := terminal.MatchAgentSessionWith(ctx, lookup, session.Agent, pane.AgentSessionKind, pane.AgentSession, session.Reference)
		if verdict != terminal.SessionMatches {
			return errors.New("pane no longer proves the lost execution session: " + detail)
		}
		return nil
	}
	expected, err := agentCommandName(lookup, session.Agent)
	if err != nil {
		return err
	}
	if pane.Dead != "0" || pane.InMode != "0" || pane.Command != expected || pane.SessionMarker != session.Reference {
		return errors.New("pane process or session differs from the lost execution")
	}
	return nil
}
func stopWorkerPane(ctx context.Context, backend terminal.Backend, program string, address terminal.Address, session launch.AgentSession, lookup config.AgentLookup) error {
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
	if err := sameWorkerPane(ctx, backend, pane, session, lookup); err != nil {
		return err
	}
	stopped := false
	if inspector, ok := backend.(terminal.ProcessInspector); ok {
		observed, inspectErr := inspector.ProcessFacts(ctx, conn, address.Pane)
		if inspectErr == nil {
			expected, err := agentCommandName(lookup, session.Agent)
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
				if err := sameWorkerPane(ctx, backend, current, session, lookup); err != nil {
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
		command, err := agentExitCommand(lookup, session.Agent)
		if err != nil {
			return err
		}
		if err := backend.DeliverText(ctx, conn, address.Pane, command); err != nil {
			return err
		}
	}
	// Exit observation does not call the container cleanup routines.
	if backend.Capabilities().AgentIdentity {
		_, err = agentWaitExit(backend, program, address, session, 10, lookup)
	} else {
		_, err = processWaitExit(backend, program, address, session, 10, lookup)
	}
	return err
}
