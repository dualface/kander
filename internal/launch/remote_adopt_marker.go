package launch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
)

// repairRemoteAdoptSession orders the non-atomic writes so a retry can prove
// any completed step. It never treats a pane marker alone as proof of a bare
// session, or replaces a marker that belongs to another session.
func repairRemoteAdoptSession(ctx context.Context, cfg *config.Config, cwd string, service *remote.Session, state api.ExecutionState, credential *remote.Credential) (bool, error) {
	task, window := credential.TaskID, credential.Window
	cardSession := board.MetadataFrom(state.Task.Text, board.FieldSession)
	behind := credential.Session != cardSession
	backend, address, ok := terminal.ParseWindow(window)
	if credential.AgentPID > 0 || !ok || !backend.Capabilities().PaneMetadata {
		if behind {
			return false, launchError("launch.start_identity_changed")
		}
		return false, nil
	}
	parsed := parseSessionValue(cardSession)
	if parsed == nil {
		return false, launchError("launch.start_identity_changed")
	}
	stop := t("launch.unmarked_executor_stop_first", window)
	reference := parsed.Reference
	if reference == "" {
		provider, err := sessionProviderOf(cfg, parsed.Agent)
		if err != nil {
			return false, launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
		}
		if !provider.Resumable() && !sessionDiscovers(provider) {
			return false, nil
		}
		repairer, ok := provider.(sessionMarkerRepairer)
		if !ok {
			return false, launchError("launch.adopt_unmarked_pane", task, stop)
		}
		reference, err = scanRemoteAdoptSession(ctx, repairer, credential, state.Task.Text, cwd, stop)
		if err != nil {
			return false, err
		}
	}
	conn, facts, err := probeRemoteAdoptPane(ctx, backend, address.Pane)
	if err != nil {
		return false, launchError("launch.adopt_marker_write_failed", task, window, err.Error(), stop)
	}
	if facts.SessionMarker != reference && (behind || facts.SessionMarker != "") {
		return false, launchError("launch.adopt_marker_conflict", task, window, stop)
	}
	writeMarker := facts.SessionMarker == ""
	writeSession := parsed.Reference == ""
	if !writeMarker && !writeSession && !behind {
		return false, nil
	}
	if _, err := recheckRemoteAdoption(ctx, service, *credential, state, cardSession, true); err != nil {
		return false, err
	}
	if writeMarker {
		if err := backend.SetSessionMarker(conn, address.Pane, reference); err != nil {
			return false, launchError("launch.adopt_marker_write_failed", task, window, err.Error(), stop)
		}
		if !command.JSON() {
			fmt.Println(t("launch.adopt_marker_written", task, window, reference))
		}
	}
	if writeSession {
		cardSession = (AgentSession{Agent: parsed.Agent, Reference: reference}).Render()
		update := api.ExecutionUpdate{Action: api.ExecutionSession, SessionBefore: credential.Session, SessionAfter: cardSession}
		if _, err := service.Project.Execution(ctx, client.NewKey(), task, update, *credential.Claim()); err != nil {
			return false, err
		}
	}
	if writeSession || behind {
		credential.Session = cardSession
		if err := remote.WriteCredential(service.Home, *credential); err != nil {
			return false, err
		}
		_ = startRemoteWorker(service.Home)
	}
	return true, nil
}

func probeRemoteAdoptPane(ctx context.Context, backend terminal.Backend, pane string) (terminal.Conn, terminal.PaneFacts, error) {
	program, err := lookPath(backend.Executable())
	if err != nil {
		return terminal.Conn{}, terminal.PaneFacts{}, err
	}
	probeCtx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	conn := terminal.Conn{Program: program, Run: terminal.ProbeRunner}
	facts, err := backend.PaneFacts(probeCtx, conn, pane)
	if err == nil && facts.Gone {
		err = launchError("launch.pane_does_not_exist_2", pane)
	}
	// SetSessionMarker has no context parameter. Keep its runner within the
	// adoption deadline as well as the default terminal probe budget.
	conn.Run = func(_ context.Context, program string, args []string) (probe.Result, error) {
		markerCtx, cancel := probe.WithDefaultTimeout(ctx)
		defer cancel()
		return terminal.ProbeRunner(markerCtx, program, args)
	}
	return conn, facts, err
}

func scanRemoteAdoptSession(ctx context.Context, repairer sessionMarkerRepairer, credential *remote.Credential, card, cwd, stop string) (string, error) {
	task := credential.TaskID
	floor, err := remoteAdoptSessionFloor(board.MetadataFrom(card, board.FieldStartedAt))
	if err != nil {
		return "", launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
	}
	launchDir := credential.Worktree
	if launchDir == "" {
		launchDir = remote.ProjectDir(cwd)
	}
	scan, err := repairer.AdoptionScan(task, launchDir, floor)
	if err != nil {
		return "", launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
	}
	candidates, err := scan.Candidates(ctx)
	if err != nil {
		return "", launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
	}
	if len(candidates) != 1 {
		return "", launchError("launch.adopt_session_unproven", task, strconv.Itoa(len(candidates)), stop)
	}
	return candidates[0], nil
}

func remoteAdoptSessionFloor(startedAt string) (time.Time, error) {
	start, err := time.ParseInLocation("2006-01-02 15:04", startedAt, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	// The card stores a server wall-clock time but relays no time zone.
	return start.Add(-codexRolloutNameSkew), nil
}

func recheckRemoteAdoption(ctx context.Context, service *remote.Session, credential remote.Credential, before api.ExecutionState, expectedSession string, beforeRepair bool) (api.ExecutionState, error) {
	current, err := service.Project.ExecutionState(ctx, credential.TaskID, *credential.Claim())
	key := "launch.adopt_changed_after_marker"
	if beforeRepair {
		key = "launch.adopt_changed_before_marker"
	}
	if err != nil {
		return api.ExecutionState{}, errors.Join(launchError(key, credential.TaskID), err)
	}
	if current.Record.ID != before.Record.ID || current.Record.Status != before.Record.Status ||
		current.Task.State != before.Task.State || beforeRepair && current.Task.Revision != before.Task.Revision ||
		board.MetadataFrom(current.Task.Text, board.FieldOwner) != before.Record.Owner ||
		board.MetadataFrom(current.Task.Text, board.FieldSession) != expectedSession ||
		board.MetadataFrom(current.Task.Text, board.FieldWindow) != credential.Window {
		return api.ExecutionState{}, launchError(key, credential.TaskID)
	}
	return current, nil
}
