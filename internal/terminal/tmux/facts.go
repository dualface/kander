package tmux

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

var goneMarkers = []string{"can't find", "no server running", "no sessions"}

func gone(detail string) bool {
	lowered := strings.ToLower(detail)
	for _, marker := range goneMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func optionMissing(detail, option string) bool {
	lowered := strings.ToLower(strings.TrimSpace(detail))
	if lowered == "" {
		return true
	}
	return lowered == "invalid option: "+option || lowered == "unknown option: "+option
}

func showPaneOption(ctx context.Context, conn terminal.Conn, paneID, option string) (value string, goneDetail string, missing bool, err error) {
	res, runErr := conn.Run(ctx, conn.Program, []string{"show-options", "-p", "-v", "-t", paneID, option})
	if runErr != nil {
		return "", "", false, runErr
	}
	detail := strings.TrimSpace(res.Stderr)
	if res.Code != 0 {
		if gone(detail) {
			return "", detail, false, nil
		}
		if optionMissing(detail, option) {
			return "", "", true, nil
		}
		return "", "", false, textError("probe.failed_to_probe_the_tmux_pane", orExit(detail, res.Code))
	}
	return strings.TrimSpace(res.Stdout), "", false, nil
}

// PaneFacts collects the command/mode/dead state of a pane plus its session
// marker, sharing one budget. It reads @kander_session, then @onevoke_session.
func (b *Backend) PaneFacts(ctx context.Context, conn terminal.Conn, paneID string) (terminal.PaneFacts, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := conn.Run(ctx, conn.Program, []string{
		"display-message", "-p", "-t", paneID,
		"#{pane_current_command}\t#{pane_in_mode}\t#{pane_dead}",
	})
	if err != nil {
		return terminal.PaneFacts{}, err
	}
	detail := strings.TrimSpace(res.Stderr)
	if res.Code != 0 {
		if gone(detail) {
			return terminal.PaneFacts{Gone: true, GoneDetail: detail}, nil
		}
		return terminal.PaneFacts{}, textError("launch.tmux_pane_does_not_exist", paneID, orExit(detail, res.Code))
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), "\t")
	if len(fields) != 3 {
		return terminal.PaneFacts{}, textError("launch.tmux_pane_probe_returned_an_invalid_response")
	}
	marker, goneDetail, err := readSessionMarker(ctx, conn, paneID)
	if err != nil {
		return terminal.PaneFacts{}, err
	}
	if goneDetail != "" {
		return terminal.PaneFacts{Gone: true, GoneDetail: goneDetail}, nil
	}
	return terminal.PaneFacts{Command: fields[0], InMode: fields[1], Dead: fields[2], SessionMarker: marker}, nil
}

func readSessionMarker(ctx context.Context, conn terminal.Conn, paneID string) (string, string, error) {
	for _, option := range []string{paneSessionOption, legacyPaneSession} {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		value, goneDetail, missing, err := showPaneOption(ctx, conn, paneID, option)
		if err != nil {
			return "", "", err
		}
		if goneDetail != "" {
			return "", goneDetail, nil
		}
		if missing {
			continue
		}
		return value, "", nil
	}
	return "", "", nil
}

// Topology reports the session/window owning the pane and its pane count.
func (b *Backend) Topology(ctx context.Context, conn terminal.Conn, address terminal.Address) (terminal.Topology, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := conn.Run(ctx, conn.Program, []string{
		"display-message", "-p", "-t", address.Pane,
		"#{session_id}\t#{session_name}\t#{window_id}\t#{window_panes}",
	})
	if err != nil {
		return terminal.Topology{}, err
	}
	if res.Code != 0 {
		detail := orExit(strings.TrimSpace(res.Stderr), res.Code)
		return terminal.Topology{}, textError("probe.failed_to_probe_the_tmux_pane_container", detail)
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), "\t")
	if len(fields) != 4 {
		return terminal.Topology{}, textError("probe.tmux_pane_container_probe_returned_an_invalid_response")
	}
	// tmux records the session id, tmux-session the session name.
	session := fields[0]
	if b.projectSession {
		session = fields[1]
	}
	return terminal.Topology{Session: session, Container: fields[2], PaneCount: fields[3]}, nil
}

// ContainerExists reports whether the window still exists.
func (b *Backend) ContainerExists(ctx context.Context, conn terminal.Conn, address terminal.Address) (bool, error) {
	res, err := conn.Run(ctx, conn.Program, []string{"display-message", "-p", "-t", address.Container, "#{window_id}"})
	if err != nil {
		return false, err
	}
	if res.Code != 0 {
		detail := strings.TrimSpace(res.Stderr)
		if gone(detail) {
			return false, nil
		}
		return false, textError("takeover.failed_to_probe_the_tmux_window", orExit(detail, res.Code))
	}
	if strings.TrimSpace(res.Stdout) != address.Container {
		return false, textError("takeover.tmux_window_probe_returned_an_invalid_response", orNA(strings.TrimSpace(res.Stdout)))
	}
	return true, nil
}

func markersMatch(kander, onevoke, reference string) bool {
	if reference == "" {
		return false
	}
	return kander == reference || onevoke == reference
}

// ReverseLookup uniquely locates a live pane by session marker and foreground
// process name, reading both the kander and onevoke markers.
func (b *Backend) ReverseLookup(ctx context.Context, conn terminal.Conn, identity terminal.Identity) (terminal.Address, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := conn.Run(ctx, conn.Program, []string{
		"list-panes", "-a", "-F",
		"#{pane_id}\t#{session_id}\t#{session_name}\t#{window_id}\t#{pane_current_command}\t#{pane_dead}\t#{@kander_session}\t#{@onevoke_session}",
	})
	if err != nil {
		return terminal.Address{}, err
	}
	if res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = config.Text("liveness.empty_output")
		}
		return terminal.Address{}, &probe.Error{Message: detail}
	}
	expected, err := identity.ProcessName()
	if err != nil {
		return terminal.Address{}, err
	}
	var matches []terminal.Address
	for _, line := range strings.Split(res.Stdout, "\n") {
		if err := ctx.Err(); err != nil {
			return terminal.Address{}, err
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		var paneID, sessionID, sessionName, windowID, command, dead, kander, onevoke string
		switch len(fields) {
		case 7:
			paneID, sessionID, sessionName, windowID, command, dead, onevoke = fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6]
		case 8:
			paneID, sessionID, sessionName, windowID, command, dead, kander, onevoke = fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6], fields[7]
		default:
			return terminal.Address{}, textError("liveness.tmux_session_lookup_returned_invalid_output")
		}
		if !windowRe.MatchString("tmux:"+sessionID+":"+windowID+":"+paneID) ||
			sessionName == "" ||
			command == "" || (dead != "0" && dead != "1") || strings.ContainsRune(line, 0) {
			return terminal.Address{}, textError("liveness.tmux_session_lookup_returned_invalid_output")
		}
		if markersMatch(kander, onevoke, identity.Reference) && dead == "0" && command == expected {
			session := sessionID
			if b.projectSession {
				session = sessionName
			}
			matches = append(matches, terminal.Address{Session: session, Container: windowID, Pane: paneID})
		}
	}
	if err := ctx.Err(); err != nil {
		return terminal.Address{}, err
	}
	if len(matches) == 0 {
		return terminal.Address{}, &terminal.MatchError{Matches: 0, Cause: textError(
			"liveness.tmux_session_lookup_found_no_match_0_panes", identity.Reference,
		)}
	}
	if len(matches) != 1 {
		return terminal.Address{}, &terminal.MatchError{Matches: len(matches), Cause: textError(
			"liveness.tmux_session_lookup_is_ambiguous_panes", identity.Reference, strconv.Itoa(len(matches)),
		)}
	}
	return matches[0], nil
}
