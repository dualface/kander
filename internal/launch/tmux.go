package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

func resolveStartLauncher(launcher string) (string, error) {
	if launcher != "auto" {
		return launcher, nil
	}
	if os.Getenv("HERDR_ENV") == "1" {
		return "herdr", nil
	}
	if runtimeWindows() {
		// Native Windows has no tmux, so auto can only resolve to herdr.
		return "", launchError(
			"launch.auto_cannot_be_resolved_not_in_herdr_on_windows",
		)
	}
	if os.Getenv("TMUX") != "" {
		return "tmux", nil
	}
	return "", launchError(
		"launch.auto_cannot_be_resolved_not_currently_in_herdr_or",
	)
}

func prepareLaunch(launcher, project, command string) (LaunchPlan, error) {
	// herdr has a native Windows build; only tmux is still POSIX-only.
	if runtimeWindows() && (launcher == "tmux" || launcher == "tmux-session") {
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

// paneLauncher reports whether this launcher hands the agent to a terminal
// container's shell, which parses the single-line command back into argv.
func paneLauncher(launcher string) bool {
	return launcher == "herdr" || launcher == "tmux" || launcher == "tmux-session"
}

// launchInvocation picks the invocation form by launcher: a terminal container
// only takes one line, so argv has to survive being parsed by a shell again;
// foreground and console spawn directly and keep native argv.
func launchInvocation(plan LaunchPlan, program process.AgentProgram, arguments []string) (process.ProcessInvocation, error) {
	if paneLauncher(plan.Launcher) {
		return newShellInvocation(program, arguments, nil)
	}
	return newInvocation(program, arguments, nil)
}

// paneCommand renders one process invocation as a single line the terminal
// container's shell can run. POSIX containers are sh-like; a herdr pane on
// Windows runs PowerShell, where a quoted executable path only runs behind the
// call operator & — otherwise the shell just prints the line back and the agent
// never starts. Argv carried in ShellEnv variables has to be assigned back in
// the pane first.
// This assumes the container shell is PowerShell on Windows (herdr's default)
// and sh-like on POSIX. If a user points herdr's default_shell at cmd or
// git-bash the line comes out wrong; the container only types the text in, it
// never reports an error.
func paneCommand(inv process.ProcessInvocation) (string, error) {
	for _, value := range inv.Argv {
		if err := rejectPaneControlChars(value); err != nil {
			return "", err
		}
	}
	for _, value := range inv.ShellEnv {
		if err := rejectPaneControlChars(value); err != nil {
			return "", err
		}
	}
	if !runtimeWindows() {
		return posixJoin(inv.Argv), nil
	}
	return powershellJoin(inv.Argv, inv.ShellEnv), nil
}

// rejectPaneControlChars: a terminal container treats the command as one typed
// line plus Enter, so a bare newline submits early and turns the remainder into
// a second command. Failing to start beats sending half a command.
func rejectPaneControlChars(value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return launchError("launch.agent_command_contains_a_control_character")
	}
	return nil
}

func powershellJoin(argv []string, shellEnv map[string]string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := make([]string, 0, len(shellEnv)+len(argv)+1)
	names := make([]string, 0, len(shellEnv))
	for name := range shellEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, "$env:"+name+"="+powershellQuote(shellEnv[name])+";")
	}
	parts = append(parts, "&")
	for _, a := range argv {
		parts = append(parts, powershellQuote(a))
	}
	return strings.Join(parts, " ")
}

// powershellQuote always single-quotes: a PowerShell single-quoted string is
// literal, and an inner single quote is escaped by doubling it.
func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
