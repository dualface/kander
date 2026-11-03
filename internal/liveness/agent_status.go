package liveness

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// AgentStatusInput is one card to observe: its raw SESSION and WINDOW values.
type AgentStatusInput struct {
	TaskID  string
	Session string
	Window  string
}

// resolveWindow parses a WINDOW value; tests replace it with fake backends.
var resolveWindow = terminal.ParseWindow

// agentTarget is one input whose WINDOW names an AgentIdentity backend, or a
// process-pane backend (process set) whose pane facts report the foreground
// command.
type agentTarget struct {
	index   int
	entry   board.Entry
	session TaskSession
	backend terminal.Backend
	address terminal.Address
	process bool
}

// ObserveAgentStatus classifies the executing agent of every input with the
// rules of `kander check`, but never searches for a moved session. It returns
// one Report per input, in input order. Each backend that implements
// terminal.AgentLister is listed once; a card whose pane is missing from that
// list, and every card of a backend without the list, is probed with
// PaneFacts. A failed list marks only the cards of that backend unknown. A
// card of a process-pane backend (tmux, dsh) is probed with PaneFacts and
// classified by the foreground command and session marker, as `kander check`
// does. The fixed worker set is joined before returning.
func ObserveAgentStatus(ctx context.Context, inputs []AgentStatusInput, options BatchOptions) []Report {
	if options.Budget <= 0 {
		options.Budget = DefaultBatchBudget
	}
	if options.Concurrency <= 0 {
		options.Concurrency = DefaultBatchConcurrency
	}
	ctx, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()
	reports := make([]Report, len(inputs))
	groups, order, probes := groupAgentTargets(inputs, reports)
	for _, name := range order {
		probes = append(probes, observeBackend(ctx, groups[name], reports)...)
	}
	probePanes(ctx, probes, reports, options.Concurrency)
	observed := time.Now().UTC()
	for index := range reports {
		reports[index].ObservedAt = observed
		reports[index].ObservationValid = reports[index].Status != Unknown
	}
	return reports
}

// groupAgentTargets reports the inputs that cannot be observed, groups the
// AgentIdentity inputs by backend in first-seen backend order, and returns the
// process-pane inputs, which are always probed one by one.
func groupAgentTargets(inputs []AgentStatusInput, reports []Report) (map[string][]agentTarget, []string, []agentTarget) {
	groups := map[string][]agentTarget{}
	var order []string
	var processes []agentTarget
	for index, input := range inputs {
		entry := board.Entry{TaskID: input.TaskID}
		session := ParseSessionValue(input.Session)
		if session == nil {
			reports[index] = report(entry, nil, Unknown, "unknown", input.Window, t("liveness.missing_or_invalid_session_metadata"), "")
			continue
		}
		backend, address, ok := resolveWindow(input.Window)
		if !ok {
			reports[index] = report(entry, session, Unknown, "unknown", input.Window, t("liveness.window_metadata_is_empty_or_invalid"), "")
			continue
		}
		target := agentTarget{index: index, entry: entry, session: *session, backend: backend, address: address}
		if caps := backend.Capabilities(); !caps.AgentIdentity {
			if caps.ForegroundProcess {
				target.process = true
				processes = append(processes, target)
				continue
			}
			reports[index] = report(entry, session, Unknown, input.Window, input.Window, t("liveness.this_launcher_has_no_probeable_address"), "")
			continue
		}
		name := backend.Name()
		if _, seen := groups[name]; !seen {
			order = append(order, name)
		}
		groups[name] = append(groups[name], target)
	}
	return groups, order, processes
}

// observeBackend classifies the cards of one backend from a single agent list
// and returns the cards that still need a PaneFacts probe.
func observeBackend(ctx context.Context, group []agentTarget, reports []Report) []agentTarget {
	backend := group[0].backend
	program, err := lookPath(backend.Executable())
	if err != nil {
		for _, target := range group {
			reports[target.index] = report(target.entry, &target.session, Unknown, backend.Name(), target.address.Container,
				t("terminal.executable_not_in_path", backend.Executable()), "")
		}
		return nil
	}
	lister, ok := backend.(terminal.AgentLister)
	if !ok {
		return group
	}
	panes, err := lister.AgentList(ctx, terminal.Conn{Program: program, Run: terminal.ProbeRunner})
	if errors.Is(err, terminal.ErrUnsupported) {
		return group
	}
	if err != nil {
		for _, target := range group {
			reports[target.index] = report(target.entry, &target.session, Unknown, backend.Name(), target.address.Container, probe.FailureDetail(err), "")
		}
		return nil
	}
	listed := make(map[string]terminal.PaneFacts, len(panes))
	for _, pane := range panes {
		listed[pane.Pane] = pane.Facts()
	}
	var missing []agentTarget
	for _, target := range group {
		facts, ok := listed[target.address.Pane]
		if !ok {
			missing = append(missing, target)
			continue
		}
		reports[target.index] = classifyAgentFacts(ctx, target.entry, target.session, backend, target.address, program, facts, staleSearch{})
	}
	return missing
}

// probePanes classifies each target with its own PaneFacts probe, bounded by
// a fixed worker set.
func probePanes(ctx context.Context, targets []agentTarget, reports []Report, concurrency int) {
	jobs := make(chan agentTarget)
	var workers sync.WaitGroup
	for n := 0; n < min(concurrency, len(targets)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range jobs {
				if err := ctx.Err(); err != nil {
					reports[target.index] = report(target.entry, &target.session, Unknown, target.backend.Name(), target.address.Container,
						t("liveness.batch_not_observed", probe.FailureDetail(err)), "")
					continue
				}
				if target.process {
					reports[target.index] = classifyProcessPane(ctx, target.entry, target.session, target.backend, target.address, staleSearch{})
					continue
				}
				reports[target.index] = classifyAgentPane(ctx, target.entry, target.session, target.backend, target.address, staleSearch{})
			}
		}()
	}
	for _, target := range targets {
		jobs <- target
	}
	close(jobs)
	workers.Wait()
}
