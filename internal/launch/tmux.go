package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func resolveStartLauncher(launcher string) (string, error) {
	if launcher != "auto" {
		return launcher, nil
	}
	if os.Getenv("HERDR_ENV") == "1" {
		return "herdr", nil
	}
	if os.Getenv("TMUX") != "" {
		return "tmux", nil
	}
	return "", launchError(
		"launch.auto_cannot_be_resolved_not_currently_in_herdr_or",
	)
}

func prepareLaunch(launcher, project, command string) (LaunchPlan, error) {
	if runtimeWindows() && (launcher == "auto" || launcher == "tmux" || launcher == "tmux-session" || launcher == "herdr") {
		return LaunchPlan{}, launchError(
			"launch.windows_does_not_support_the_launcher_use_console_or", launcher,
		)
	}
	resolved, err := resolveStartLauncher(launcher)
	if err != nil {
		return LaunchPlan{}, err
	}
	plan := LaunchPlan{Launcher: resolved, Project: project}
	switch resolved {
	case "tmux", "tmux-session":
		tmux, err := lookPath("tmux")
		if err != nil {
			return LaunchPlan{}, launchError("launch.tmux_is_not_in_path_run_kander_welcome_to")
		}
		plan.Tmux = tmux
		if resolved == "tmux" {
			session, err := tmuxSessionID(tmux)
			if err != nil {
				return LaunchPlan{}, err
			}
			plan.Session = session
		} else {
			session, exists, err := resolveProjectSession(tmux, project)
			if err != nil {
				return LaunchPlan{}, err
			}
			plan.Session = session
			plan.SessionExists = exists
		}
	case "herdr":
		if os.Getenv("HERDR_ENV") != "1" {
			return LaunchPlan{}, launchError(
				"launch.not_currently_in_herdr_the_herdr_launcher_requires_herdr",
			)
		}
		herdr, err := lookPath("herdr")
		if err != nil {
			return LaunchPlan{}, launchError(
				"launch.herdr_is_not_in_path_run_kander_welcome_to",
			)
		}
		plan.HerdrBin = herdr
		ws := strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID"))
		if ws == "" {
			return LaunchPlan{}, launchError(
				"launch.herdr_workspace_id_is_missing_cannot_create_a_tab",
			)
		}
		plan.HerdrWorkspace = ws
	case "console":
		if !runtimeWindows() {
			return LaunchPlan{}, launchError("launch.the_console_launcher_is_available_only_on_windows")
		}
	default:
		if !(stdinIsTTY() && stdoutIsTTY() && stderrIsTTY()) {
			fallback := "tmux"
			if runtimeWindows() {
				fallback = "console"
			}
			return LaunchPlan{}, launchError(
				"launch.foreground_mode_requires_an_interactive_terminal_stdin_stdout_stderr", command, fallback,
			)
		}
	}
	return plan, nil
}

type cmdResult struct {
	Code   int
	Stdout string
	Stderr string
}

func runCaptured(bin string, args []string, timeout time.Duration) (cmdResult, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
		defer timer.Stop()
	}
	err := cmd.Run()
	res := cmdResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

func tmuxCapture(tmux string, args ...string) cmdResult {
	res, err := runCaptured(tmux, args, 0)
	if err != nil {
		res.Stderr = err.Error()
		if res.Code == 0 {
			res.Code = 1
		}
	}
	return res
}

func tmuxSessionID(tmux string) (string, error) {
	pane := os.Getenv("TMUX_PANE")
	if os.Getenv("TMUX") == "" || pane == "" {
		return "", launchError(
			"launch.not_currently_in_a_tmux_session_run", tmuxSessionHint,
		)
	}
	res := tmuxCapture(tmux, "display-message", "-p", "-t", pane, "#{session_id}")
	session := strings.TrimSpace(res.Stdout)
	if res.Code != 0 || !regexp.MustCompile(`^\$\d+$`).MatchString(session) {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = t("launch.cannot_determine_the_current_session")
		}
		return "", launchError("launch.failed_to_read_tmux_session", detail)
	}
	return session, nil
}

func projectSessionName(project string) string {
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

func sessionOwner(tmux, session string) (string, bool) {
	if tmuxCapture(tmux, "has-session", "-t", "="+session).Code != 0 {
		return "", false
	}
	res := tmuxCapture(tmux, "show-options", "-v", "-t", session, projectSessionOpt)
	if res.Code == 0 {
		return strings.TrimSpace(res.Stdout), true
	}
	legacy := tmuxCapture(tmux, "show-options", "-v", "-t", session, legacyProjectOpt)
	if legacy.Code == 0 {
		return strings.TrimSpace(legacy.Stdout), true
	}
	return "", true
}

func resolveProjectSession(tmux, project string) (string, bool, error) {
	base := projectSessionName(project)
	for index := 1; index <= projectSessionTries; index++ {
		candidate := base
		if index > 1 {
			candidate = base + "-" + itoa(index)
		}
		owner, exists := sessionOwner(tmux, candidate)
		if !exists {
			return candidate, false, nil
		}
		if owner == "" || owner == project {
			return candidate, true, nil
		}
	}
	return "", false, launchError(
		"launch.no_project_session_name_is_available_and_its_numbered", base,
	)
}

func tmuxLaunch(tmux, session string, create bool, cwd, name string) cmdResult {
	args := []string{"new-window", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-t", session + ":", "-c", cwd, "-n", name}
	if create {
		args = []string{"new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", session, "-c", cwd, "-n", name}
	}
	return tmuxCapture(tmux, args...)
}

func tmuxStartPane(tmux, pane, command string) error {
	res := tmuxCapture(tmux, "respawn-pane", "-k", "-t", pane, command)
	if res.Code != 0 {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = "exit " + itoa(res.Code)
		}
		return launchError("launch.tmux_failed_to_start_agent", detail)
	}
	return nil
}

func tmuxSetPaneSession(tmux, pane, session string) error {
	res := tmuxCapture(tmux, "set-option", "-p", "-t", pane, paneSessionOption, session)
	if res.Code != 0 {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = "exit " + itoa(res.Code)
		}
		return launchError("launch.tmux_failed_to_record_the_pane_session", detail)
	}
	return nil
}

func tmuxCloseWindow(tmux, window string) string {
	if window == "" {
		return ""
	}
	res, err := runCaptured(tmux, []string{"kill-window", "-t", window}, probe.DefaultCommandTimeout)
	if err != nil {
		return t("launch.failed_to_close_tmux_window", err.Error())
	}
	if res.Code == 0 {
		return ""
	}
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = "exit " + itoa(res.Code)
	}
	return t("launch.failed_to_close_tmux_window", detail)
}

func tmuxNotifyTarget(tmux, pane string, session AgentSession, timeout time.Duration) error {
	paneProbe, err := probe.ProbeTmuxPaneWithin(tmux, pane, timeout)
	if err != nil {
		return err
	}
	if paneProbe.Facts == nil {
		return launchError("launch.tmux_pane_does_not_exist", pane, paneProbe.GoneDetail)
	}
	facts := paneProbe.Facts
	if facts.Dead != "0" {
		return launchError("launch.tmux_pane_is_dead", pane)
	}
	if facts.InMode != "0" {
		return launchError("launch.tmux_pane_is_in_copy_mode", pane)
	}
	expected := filepathBase(configAgentExe(session.Agent))
	if facts.Command != expected {
		return launchError(
			"launch.tmux_foreground_process_mismatch_expected_actual", expected, orNA(facts.Command),
		)
	}
	if facts.SessionMarker == "" {
		return launchError("launch.tmux_pane_has_no_session_marker", pane)
	}
	if facts.SessionMarker != session.Reference {
		return launchError(
			"launch.tmux_session_mismatch_task_pane", session.Reference, facts.SessionMarker,
		)
	}
	return nil
}

func orExit(detail string, code int) string {
	if detail == "" {
		return "exit " + itoa(code)
	}
	return detail
}

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}

func configAgentExe(agent string) string {
	return lookPathName(agent)
}

func lookPathName(agent string) string {
	return config.AgentExecutableName(agent)
}

func posixJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = posixQuote(a)
	}
	return strings.Join(parts, " ")
}

func posixQuote(s string) string {
	if s == "" {
		return "''"
	}
	if regexp.MustCompile(`^[A-Za-z0-9_./:=+-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
