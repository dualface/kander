package launch

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

func resolveStartLauncher(launcher string) (string, error) {
	if launcher != terminal.Auto {
		return launcher, nil
	}
	if backend, ok := terminal.ResolveAuto(runtimeWindows(), os.Getenv); ok {
		return backend.Name(), nil
	}
	if runtimeWindows() {
		return "", launchError(
			"launch.auto_cannot_be_resolved_not_in_herdr_on_windows",
		)
	}
	return "", launchError(
		"launch.auto_cannot_be_resolved_not_currently_in_herdr_or",
	)
}

func prepareLaunch(launcher, project, command string) (LaunchPlan, error) {
	if runtimeWindows() && terminal.HasCapability(launcher, func(c terminal.Capabilities) bool { return c.POSIXOnly }) {
		return LaunchPlan{}, launchError(
			"launch.windows_does_not_support_the_launcher_use_console_or", launcher,
		)
	}
	resolved, err := resolveStartLauncher(launcher)
	if err != nil {
		return LaunchPlan{}, err
	}
	plan := LaunchPlan{Launcher: resolved}
	target, err := plan.backend().Prepare(terminal.PrepareRequest{
		Project:  project,
		Command:  command,
		Windows:  runtimeWindows(),
		LookPath: lookPath,
		Getenv:   os.Getenv,
		TTY:      func() bool { return stdinIsTTY() && stdoutIsTTY() && stderrIsTTY() },
	})
	if err != nil {
		return LaunchPlan{}, err
	}
	plan.Target = target
	return plan, nil
}

// processNotifyTarget validates a resumed agent in a pane identified by its
// foreground process and session marker.
func processNotifyTarget(plan LaunchPlan, pane string, session AgentSession, timeout time.Duration) error {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	facts, err := plan.backend().PaneFacts(ctx, probeConn(plan), pane)
	if err != nil {
		return err
	}
	if facts.Gone {
		return launchError("launch.tmux_pane_does_not_exist", pane, facts.GoneDetail)
	}
	if facts.Dead != "0" {
		return launchError("launch.tmux_pane_is_dead", pane)
	}
	if facts.InMode != "0" {
		return launchError("launch.tmux_pane_is_in_copy_mode", pane)
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	if !config.HasAgent(cfg, session.Agent) {
		return launchError("launch.unsupported_agent", session.Agent)
	}
	expected := config.AgentFor(cfg, session.Agent).ProcessName
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

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}

// launchInvocation picks the invocation form by launcher: a terminal container
// only takes one line, so argv has to survive being parsed by a shell again;
// foreground and console spawn directly and keep native argv.
func launchInvocation(plan LaunchPlan, program process.AgentProgram, arguments []string) (process.ProcessInvocation, error) {
	if plan.capabilities().Container {
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
