package launch

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

func init() {
	cli.Commands["start"] = RunStart
	cli.Commands["resume"] = RunResume
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

func usage(w io.Writer, cmd string) {
	if cmd == "start" {
		fmt.Fprintln(w, t(
			"launch.usage_kander_start_agent_codex_claude_grok_cursor_launcher",
		))
		return
	}
	fmt.Fprintln(w, t(
		"launch.usage_kander_resume_agent_timeout_seconds_message_text_message",
	))
}

func usageFail(cmd, id string, args ...any) int {
	usage(os.Stderr, cmd)
	if id != "" {
		fmt.Fprintln(os.Stderr, t(id, args...))
	}
	return 2
}

func takeValue(args []string, i int) (string, int, bool) {
	if i+1 >= len(args) {
		return "", i, false
	}
	return args[i+1], i + 1, true
}

func parseAgentLauncher(args []string) (rest []string, agent, launcher string, agentSet bool, message string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--agent":
			val, next, ok := takeValue(args, i)
			if !ok {
				return nil, "", "", false, t("launch.missing_agent_value")
			}
			if !contains(config.ExecutionAgents, val) {
				return nil, "", "", false, t("launch.unknown_agent", val)
			}
			agent, agentSet, i = val, true, next
		case strings.HasPrefix(arg, "--agent="):
			val := strings.TrimPrefix(arg, "--agent=")
			if !contains(config.ExecutionAgents, val) {
				return nil, "", "", false, t("launch.unknown_agent", val)
			}
			agent, agentSet = val, true
		case arg == "--launcher":
			val, next, ok := takeValue(args, i)
			if !ok {
				return nil, "", "", false, t("launch.missing_launcher_value")
			}
			if !contains(config.Launchers, val) {
				return nil, "", "", false, t("launch.unknown_launcher", val)
			}
			launcher, i = val, next
		case strings.HasPrefix(arg, "--launcher="):
			val := strings.TrimPrefix(arg, "--launcher=")
			if !contains(config.Launchers, val) {
				return nil, "", "", false, t("launch.unknown_launcher", val)
			}
			launcher = val
		default:
			rest = append(rest, arg)
		}
	}
	return rest, agent, launcher, agentSet, ""
}

// RunStart implements kander start.
func RunStart(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout, "start")
			return 0
		}
	}
	rest, agent, launcher, _, message := parseAgentLauncher(args)
	if message != "" {
		usage(os.Stderr, "start")
		fmt.Fprintln(os.Stderr, message)
		return 2
	}
	var task string
	for _, arg := range rest {
		if strings.HasPrefix(arg, "-") {
			return usageFail("start", "board.unknown_option", arg)
		}
		if task != "" {
			return usageFail("start", "board.too_many_arguments")
		}
		task = arg
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	if err := commandStart(root, agent, launcher, task); err != nil {
		return fail(err)
	}
	return 0
}

// RunResume implements kander resume.
func RunResume(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout, "resume")
			return 0
		}
	}
	timeout := notifyDefaultTimeout
	var message, messageFile, agent, launcher string
	var messageSet, agentSet bool
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--message":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_message_value")
			}
			message, messageSet, i = val, true, next
		case strings.HasPrefix(arg, "--message="):
			message, messageSet = strings.TrimPrefix(arg, "--message="), true
		case arg == "--message-file":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_message_file_value")
			}
			messageFile, i = val, next
		case strings.HasPrefix(arg, "--message-file="):
			messageFile = strings.TrimPrefix(arg, "--message-file=")
		case arg == "--timeout":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_timeout_value")
			}
			n, err := strconv.ParseFloat(val, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(launchError("launch.resume_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout, i = n, next
		case strings.HasPrefix(arg, "--timeout="):
			n, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--timeout="), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(launchError("launch.resume_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout = n
		case arg == "--agent":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_agent_value")
			}
			if !contains(config.ExecutionAgents, val) {
				return usageFail("resume", "launch.unknown_agent", val)
			}
			agent, agentSet, i = val, true, next
		case strings.HasPrefix(arg, "--agent="):
			val := strings.TrimPrefix(arg, "--agent=")
			if !contains(config.ExecutionAgents, val) {
				return usageFail("resume", "launch.unknown_agent", val)
			}
			agent, agentSet = val, true
		case arg == "--launcher":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_launcher_value")
			}
			if !contains(config.Launchers, val) {
				return usageFail("resume", "launch.unknown_launcher", val)
			}
			launcher, i = val, next
		case strings.HasPrefix(arg, "--launcher="):
			val := strings.TrimPrefix(arg, "--launcher=")
			if !contains(config.Launchers, val) {
				return usageFail("resume", "launch.unknown_launcher", val)
			}
			launcher = val
		default:
			if strings.HasPrefix(arg, "-") {
				return usageFail("resume", "board.unknown_option", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return usageFail("resume", "launch.task_id_is_required")
	}
	var agentPtr *string
	if agentSet {
		agentPtr = &agent
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	if err := commandResume(root, agentPtr, launcher, positional[0], message, messageFile, messageSet, timeout); err != nil {
		return fail(err)
	}
	return 0
}
