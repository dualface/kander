package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

// RepairMissingSession repairs absent pane identities or finishes a proven
// agent-only card binding after a partial report. It never supplies
// delivery or stopped evidence by itself: callers repeat their normal checks.
// The snapshot cursor pins the revision and dispatch authorization throughout.
func RepairMissingSession(ctx context.Context, root string, snapshot board.Snapshot, override string) (board.Snapshot, error) {
	return repairMissingSession(ctx, root, snapshot, override, process.ObserveOpenFiles)
}

func repairMissingSession(ctx context.Context, root string, snapshot board.Snapshot, override string, observe func(context.Context, int) (process.ProcessFiles, error)) (board.Snapshot, error) {
	value := board.MetadataFrom(snapshot.Text, board.FieldWindow)
	backend, address, ok := terminal.ParseWindow(value)
	if !ok || !backend.Capabilities().AgentIdentity {
		return snapshot, nil
	}
	if override != "" {
		address.Pane = override
	}
	conn := terminal.Conn{Run: terminal.ProbeRunner}
	var err error
	conn.Program, err = exec.LookPath(backend.Executable())
	if err != nil {
		return snapshot, nil
	}
	facts, err := backend.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		if ctx.Err() != nil {
			return snapshot, ctx.Err()
		}
		return snapshot, nil
	}
	session := parseTaskSession(snapshot.Text)
	if session == nil || facts.Gone || facts.Agent != session.Agent {
		return snapshot, nil
	}
	if facts.AgentSession != "" && (session.Reference != "" || (facts.AgentSessionKind != "" && facts.AgentSessionKind != "id")) {
		return snapshot, nil
	}
	if facts.AgentSessionKind != "" && facts.AgentSessionKind != "id" {
		return snapshot, repairError("launch.session_repair_unresolved_native")
	}
	cfg, err := config.Load(false)
	if err != nil {
		return snapshot, err
	}
	definition := config.AgentFor(cfg, session.Agent)
	if definition.Session != nil && definition.Session.Mode == "none" {
		return snapshot, nil
	}
	if definition.Session == nil || definition.Session.Mode != "hook:codex-rollout" || !backend.Capabilities().SessionReport {
		if facts.AgentSession != "" {
			return snapshot, nil
		}
		return snapshot, repairError("launch.session_repair_unsupported")
	}
	inspector, ok := backend.(terminal.ProcessInspector)
	if !ok {
		return snapshot, repairError("launch.session_repair_unsupported")
	}
	reference := session.Reference
	if reference == "" {
		reference = facts.AgentSession
	}
	proof, err := observeBoundSession(ctx, inspector, conn, address.Pane, definition.ProcessName, snapshot.Entry.TaskID, reference, observe)
	if err != nil {
		return snapshot, err
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := observeBoundSession(ctx, inspector, conn, address.Pane, definition.ProcessName, snapshot.Entry.TaskID, proof.reference, observe)
		if err != nil {
			return err
		}
		if !sameSessionProof(proof, current) {
			return repairError("launch.session_repair_changed")
		}
		text, err := board.ReadDocumentContext(ctx, snapshot.Entry)
		if err != nil {
			return err
		}
		if text != snapshot.Text {
			return repairError("launch.session_repair_changed")
		}
		return nil
	}
	if err := verify(); err != nil {
		return snapshot, err
	}
	latest, err := backend.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return snapshot, err
	}
	if latest.Gone || latest.Agent != session.Agent || latest.Container != facts.Container {
		return snapshot, repairError("launch.session_repair_changed")
	}
	if latest.AgentSession == "" {
		if latest.AgentSessionKind != "" && latest.AgentSessionKind != "id" {
			return snapshot, repairError("launch.session_repair_changed")
		}
		if _, err := board.ReadDocumentContext(ctx, snapshot.Entry); err != nil {
			return snapshot, err
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(sessionReportBudget)
		}
		if err := backend.ReportSession(terminal.SessionReport{
			Context: ctx, Conn: conn, Pane: address.Pane, Agent: session.Agent,
			Reference: proof.reference, Deadline: deadline, Now: time.Now,
		}); err != nil {
			return snapshot, repairError("launch.session_repair_report_failed", err.Error())
		}
	}
	latest, err = backend.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return snapshot, err
	}
	verdict, _ := terminal.MatchAgentSession(ctx, session.Agent, latest.AgentSessionKind, latest.AgentSession, proof.reference)
	if latest.Gone || latest.Agent != session.Agent || latest.Container != facts.Container || verdict != terminal.SessionMatches {
		return snapshot, repairError("launch.session_repair_changed")
	}
	if err := verify(); err != nil {
		return snapshot, err
	}
	if session.Reference == "" {
		updated, err := renderSessionMetadata(snapshot.Text, (AgentSession{Agent: session.Agent, Reference: proof.reference}).Render())
		if err != nil {
			return snapshot, err
		}
		if err := board.WriteManagedDocumentContext(ctx, root, snapshot.Entry, updated); err != nil {
			return snapshot, err
		}
		snapshot.Text = updated
		snapshot.Revision++
	}
	return snapshot, nil
}

func repairError(id string, args ...any) error {
	return launchError(id, args...)
}

func observeBoundSession(ctx context.Context, inspector terminal.ProcessInspector, conn terminal.Conn, pane, name, task, reference string, observe func(context.Context, int) (process.ProcessFiles, error)) (sessionProof, error) {
	processes, err := inspector.ProcessFacts(ctx, conn, pane)
	if err != nil {
		if errors.Is(err, terminal.ErrUnsupported) {
			return sessionProof{}, repairError("launch.session_repair_unsupported")
		}
		return sessionProof{}, err
	}
	var candidates []terminal.ForegroundProcess
	for _, candidate := range processes {
		if candidate.Name == name {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) > 1 {
		return sessionProof{}, repairError("launch.session_repair_process_ambiguous")
	}
	if len(candidates) == 0 {
		return sessionProof{}, repairError("launch.session_repair_binding_missing")
	}
	files, err := observe(ctx, candidates[0].PID)
	if err != nil {
		return sessionProof{}, repairError("launch.session_repair_process_unavailable", err.Error())
	}
	return proveOpenSession(ctx, files, task, reference)
}

type sessionProof struct {
	reference string
	process   string
	path      string
	info      os.FileInfo
}

func sameSessionProof(before, after sessionProof) bool {
	return before.reference == after.reference && before.process == after.process && before.path == after.path && os.SameFile(before.info, after.info)
}
