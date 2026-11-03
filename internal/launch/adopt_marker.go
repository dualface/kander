package launch

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// codexRolloutHook is the session mode whose sessions adoption can find again:
// a rollout names its task, its launch directory and its creation time.
const codexRolloutHook = "hook:codex-rollout"

// markerRepairable reports from static facts alone whether adoption can write
// the session marker a launch left out: the card names the session, or the
// agent's sessions can be found again. It runs no discovery and writes nothing,
// so recovery hints may call it. A configuration that cannot be read keeps the
// refusal.
func markerRepairable(session string) bool {
	parsed := parseSessionValue(session)
	if parsed == nil {
		return false
	}
	if parsed.Reference != "" {
		return true
	}
	cfg, err := loadEffective()
	if err != nil {
		return false
	}
	definition := config.AgentFor(cfg, parsed.Agent)
	return definition.Session != nil && definition.Session.Mode == codexRolloutHook
}

// repairSessionMarker writes the session marker of an unmarked pane that runs
// the card's agent. observed is the snapshot the probe read and record its
// unfinished start attempt. The marker goes on the pane only: the card and the
// attempt stay as the launch left them, the shape of a launch that succeeded.
func repairSessionMarker(root string, observed board.Snapshot, record board.ExecutionRecord) error {
	task := observed.Entry.TaskID
	window := board.MetadataFrom(observed.Text, board.FieldWindow)
	reference, err := provableReference(root, observed, record)
	if err != nil {
		return err
	}
	// A takeover between the probe and the write would leave the marker on the
	// pane of an attempt that already ended, so the write waits for this check.
	// The fence of the adoption still refuses a takeover that lands after it.
	if err = requireAdoptTarget(root, observed, record); err != nil {
		return err
	}
	backend, address, ok := terminal.ParseWindow(window)
	if !ok || !backend.Capabilities().PaneMetadata {
		return launchError("launch.adopt_unmarked_pane", task, t("launch.unmarked_executor_stop_first", orNA(window)))
	}
	program, err := lookPath(backend.Executable())
	if err != nil {
		return launchError("launch.adopt_marker_write_failed", task, window, err.Error(), t("launch.unmarked_executor_stop_first", window))
	}
	conn := terminal.Conn{Program: program, Run: terminal.SpawnRunnerWithin(probe.DefaultCommandTimeout)}
	if err = backend.SetSessionMarker(conn, address.Pane, reference); err != nil {
		return launchError("launch.adopt_marker_write_failed", task, window, err.Error(), t("launch.unmarked_executor_stop_first", window))
	}
	fmt.Println(t("launch.adopt_marker_written", task, window, reference))
	return nil
}

// provableReference returns the session the unmarked pane runs. A card that
// names its session gives it directly. Otherwise markerRepairable admitted only
// a Codex agent, whose rollouts are scanned once: exactly one created since this
// attempt began, for this task and launch directory, proves the session.
// A floor or a scan that fails proves nothing either, and refuses with the same
// way out.
func provableReference(root string, observed board.Snapshot, record board.ExecutionRecord) (string, error) {
	task := observed.Entry.TaskID
	if session := parseTaskSession(observed.Text); session != nil && session.Reference != "" {
		return session.Reference, nil
	}
	stop := t("launch.unmarked_executor_stop_first", orNA(board.MetadataFrom(observed.Text, board.FieldWindow)))
	floor, err := board.ExecutionStartFloor(root, record)
	if err != nil {
		return "", launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
	}
	candidates, err := findAdoptSessionsFn(task, floor, parentDir(root))
	if err != nil {
		return "", launchError("launch.adopt_session_scan_failed", task, err.Error(), stop)
	}
	if len(candidates) != 1 {
		return "", launchError("launch.adopt_session_unproven", task, strconv.Itoa(len(candidates)), stop)
	}
	return candidates[0], nil
}

// findCodexAdoptSessions lists the Codex sessions a launch of task could have
// created since floor in cwd, in one scan with the conditions of discovery.
func findCodexAdoptSessions(task string, floor time.Time, cwd string) ([]string, error) {
	root, err := filepath.Abs(codexSessionsRoot())
	if err != nil {
		return nil, err
	}
	ctx, cancel := probe.WithDefaultTimeout(context.Background())
	defer cancel()
	return newCodexRolloutScan(root, codexPromptPrefixes(task), floor, cwd).candidates(ctx, nowFn())
}

// requireAdoptTarget refuses when the attempt or the card changed after the
// probe: another record, an ended attempt, or another OWNER, SESSION or WINDOW.
func requireAdoptTarget(root string, observed board.Snapshot, record board.ExecutionRecord) error {
	task := observed.Entry.TaskID
	current, err := board.ReadSnapshot(root, task)
	if err != nil {
		return err
	}
	latest, exists, err := board.CurrentExecution(root, task)
	if err != nil {
		return err
	}
	if !exists || latest.ID != record.ID || !latest.StartUnfinished() {
		return launchError("launch.adopt_changed_before_marker", task)
	}
	for _, field := range []string{board.FieldOwner, board.FieldSession, board.FieldWindow} {
		if board.MetadataFrom(current.Text, field) != board.MetadataFrom(observed.Text, field) {
			return launchError("launch.adopt_changed_before_marker", task)
		}
	}
	return nil
}
