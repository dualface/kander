package tmux

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func (b *Backend) Prepare(request terminal.PrepareRequest) (terminal.Target, error) {
	program, err := request.LookPath(Executable)
	if err != nil {
		return terminal.Target{}, textError("launch.tmux_is_not_in_path_run_kander_welcome_to")
	}
	conn := terminal.Conn{Program: program, Run: terminal.SpawnRunner}
	target := terminal.Target{Program: program, Project: request.Project}
	if !b.projectSession {
		session, err := currentSessionID(conn, request.Getenv)
		if err != nil {
			return terminal.Target{}, err
		}
		target.Session = session
		return target, nil
	}
	session, exists, err := resolveProjectSession(conn, request.Project)
	if err != nil {
		return terminal.Target{}, err
	}
	target.Session, target.SessionExists = session, exists
	return target, nil
}

// capture runs a launch-time tmux command without a deadline and folds a run
// failure into the result, so callers only inspect the exit status.
func capture(conn terminal.Conn, args ...string) probe.Result {
	res, err := conn.Run(background(), conn.Program, args)
	if err != nil {
		res.Stderr = err.Error()
		if res.Code == 0 {
			res.Code = 1
		}
	}
	return res
}

func currentSessionID(conn terminal.Conn, getenv func(string) string) (string, error) {
	pane := getenv("TMUX_PANE")
	if getenv("TMUX") == "" || pane == "" {
		return "", textError("launch.not_currently_in_a_tmux_session_run", SessionHint)
	}
	res := capture(conn, "display-message", "-p", "-t", pane, "#{session_id}")
	session := strings.TrimSpace(res.Stdout)
	if res.Code != 0 || !regexp.MustCompile(`^\$\d+$`).MatchString(session) {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = config.Text("launch.cannot_determine_the_current_session")
		}
		return "", textError("launch.failed_to_read_tmux_session", detail)
	}
	return session, nil
}

// ProjectSessionName derives the dedicated session name of a project path.
func ProjectSessionName(project string) string {
	base := filepathBase(project)
	var b strings.Builder
	for _, r := range base {
		if !unicode.IsPrint(r) {
			continue
		}
		if unicode.IsSpace(r) || r == '.' || r == ':' {
			b.WriteByte('-')
			continue
		}
		b.WriteRune(r)
	}
	label := strings.Trim(b.String(), "-")
	label = regexp.MustCompile(`-{2,}`).ReplaceAllString(label, "-")
	sum := sha256.Sum256([]byte(project))
	digest := hex.EncodeToString(sum[:])[:8]
	if label == "" {
		return "kb-" + digest
	}
	if len([]rune(label)) > 30 {
		label = string([]rune(label)[:30])
	}
	return "kb-" + label + "-" + digest
}

func filepathBase(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func sessionOwner(conn terminal.Conn, session string) (string, bool) {
	if capture(conn, "has-session", "-t", "="+session).Code != 0 {
		return "", false
	}
	res := capture(conn, "show-options", "-v", "-t", session, projectSessionOpt)
	if res.Code == 0 {
		return strings.TrimSpace(res.Stdout), true
	}
	legacy := capture(conn, "show-options", "-v", "-t", session, legacyProjectOpt)
	if legacy.Code == 0 {
		return strings.TrimSpace(legacy.Stdout), true
	}
	return "", true
}

func resolveProjectSession(conn terminal.Conn, project string) (string, bool, error) {
	base := ProjectSessionName(project)
	for index := 1; index <= projectSessionTries; index++ {
		candidate := base
		if index > 1 {
			candidate = base + "-" + strconv.Itoa(index)
		}
		owner, exists := sessionOwner(conn, candidate)
		if !exists {
			return candidate, false, nil
		}
		if owner == "" || owner == project {
			return candidate, true, nil
		}
	}
	return "", false, textError("launch.no_project_session_name_is_available_and_its_numbered", base)
}

func openWindow(conn terminal.Conn, session string, create bool, cwd, name string) probe.Result {
	args := []string{"new-window", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-t", session + ":", "-c", cwd, "-n", name}
	if create {
		args = []string{"new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", session, "-c", cwd, "-n", name}
	}
	return capture(conn, args...)
}

// CreateContainer opens a background window (or the project session itself
// when it does not exist yet) and returns its window and pane.
func (b *Backend) CreateContainer(conn terminal.Conn, target terminal.Target, cwd, label string) (terminal.Address, error) {
	create := b.projectSession && !target.SessionExists
	result := openWindow(conn, target.Session, create, cwd, label)
	if create && result.Code != 0 {
		owner, exists := sessionOwner(conn, target.Session)
		if exists && (owner == "" || owner == target.Project) {
			create = false
			result = openWindow(conn, target.Session, false, cwd, label)
		}
	}
	if result.Code != 0 {
		sub := "new-window"
		if create {
			sub = "new-session"
		}
		detail := orExit(trimNL(result.Stderr), result.Code)
		return terminal.Address{}, textError("launch.tmux_failed", sub, detail)
	}
	parts := strings.Split(trimNL(result.Stdout), "\t")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return terminal.Address{}, textError("launch.tmux_launch_failed_window_pane_id_was_not_returned")
	}
	if create {
		_ = capture(conn, "set-option", "-t", target.Session, projectSessionOpt, target.Project)
	}
	return terminal.Address{Session: target.Session, Container: parts[0], Pane: parts[1]}, nil
}

// WaitReady is immediate: respawn-pane replaces the placeholder pane process.
func (b *Backend) WaitReady(terminal.Conn, string) error { return nil }

func (b *Backend) RunCommand(conn terminal.Conn, pane, command string, posix bool) error {
	if posix {
		// tmux may wrap the command in default-shell -c. Shells such as dash
		// do not optimize the last command into exec, leaving the shell as
		// pane_current_command and breaking agent liveness checks.
		command = "exec " + command
	}
	res := capture(conn, "respawn-pane", "-k", "-t", pane, command)
	if res.Code != 0 {
		return textError("launch.tmux_failed_to_start_agent", orExit(strings.TrimSpace(res.Stderr), res.Code))
	}
	return nil
}

func (b *Backend) SetSessionMarker(conn terminal.Conn, pane, value string) error {
	res := capture(conn, "set-option", "-p", "-t", pane, paneSessionOption, value)
	if res.Code != 0 {
		return textError("launch.tmux_failed_to_record_the_pane_session", orExit(strings.TrimSpace(res.Stderr), res.Code))
	}
	return nil
}

func (b *Backend) ReportSession(terminal.SessionReport) error { return terminal.ErrUnsupported }
