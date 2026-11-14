package launch

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	cli.Commands["start"] = RunStart
	cli.RemoteCommands["start"] = RunStartRemote
	cli.Commands["resume"] = RunResume
	cli.RemoteCommands["resume"] = cli.RequireService(RunResumeRemote)
	cli.Commands["dispatch"] = RunDispatch
	cli.RemoteCommands["dispatch"] = cli.RequireService(RunDispatchRemote)
	cli.Commands["coordinator"] = RunCoordinator
	cli.RemoteCommands["coordinator"] = cli.RequireService(RunCoordinatorRemote)
	cli.Commands["orchestrate"] = RunOrchestrate
	cli.RemoteCommands["orchestrate"] = cli.RequireService(RunOrchestrateRemote)
	// The issue command front end never imports this package; the takeover
	// starter is wired here so every binary that can start agents also supports
	// `kander issue triage` and the TUI's `s` confirmation.
	issue.SetTriageStarter(StartTriage)
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

func usage(w io.Writer, cmd string) {
	fmt.Fprintln(w, config.CommandUsage(cmd))
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
			if !config.ValidAgentName(val) {
				return nil, "", "", false, t("launch.unknown_agent", val)
			}
			agent, agentSet, i = val, true, next
		case strings.HasPrefix(arg, "--agent="):
			val := strings.TrimPrefix(arg, "--agent=")
			if !config.ValidAgentName(val) {
				return nil, "", "", false, t("launch.unknown_agent", val)
			}
			agent, agentSet = val, true
		case arg == "--launcher":
			val, next, ok := takeValue(args, i)
			if !ok {
				return nil, "", "", false, t("launch.missing_launcher_value")
			}
			if !config.ValidLauncherName(val) {
				return nil, "", "", false, t("launch.unknown_launcher", val)
			}
			launcher, i = val, next
		case strings.HasPrefix(arg, "--launcher="):
			val := strings.TrimPrefix(arg, "--launcher=")
			if !config.ValidLauncherName(val) {
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
	if _, err := config.Load(false); err != nil {
		return fail(err)
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	if err := commandStart(root, agent, launcher, task); err != nil {
		return exitLaunch(err)
	}
	return 0
}

// RunStartRemote implements kander start in server mode.
func RunStartRemote(args []string) int {
	rest, agent, launcher, _, message := parseAgentLauncher(args)
	if message != "" {
		usage(os.Stderr, "start")
		fmt.Fprintln(os.Stderr, message)
		return 2
	}
	if len(rest) > 1 || len(rest) == 1 && strings.HasPrefix(rest[0], "-") {
		return usageFail("start", "launch.task_id_is_required")
	}
	var taskID string
	if len(rest) == 0 {
		cfg, err := config.Load(false)
		if err != nil {
			return fail(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return fail(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
		defer cancel()
		session, err := remote.Open(ctx, cfg, cwd)
		if err != nil {
			return fail(err)
		}
		selected, err := remote.SelectTask(ctx, session, "todo", os.Stdin, os.Stdout)
		if err != nil {
			return fail(err)
		}
		taskID = selected.TaskID
	} else {
		taskID = rest[0]
	}
	result, err := StartRemote(agent, launcher, taskID)
	for _, warning := range result.Warnings {
		fmt.Fprint(os.Stderr, warning)
	}
	if err != nil {
		return exitLaunch(err)
	}
	if result.Superseded {
		stepAside(result.TaskID, result.Plan, result.Outcome)
		return 0
	}
	if err := reportLaunch(t("launch.started"), board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome); err != nil {
		return exitLaunch(err)
	}
	return 0
}

// RunResume implements kander resume.
func RunResume(args []string) int { return runServiceResume(args, false) }

// RunResumeRemote is the mandatory REST entrance used by the client binary.
func RunResumeRemote(args []string) int { return runServiceResume(args, true) }

func runServiceResume(args []string, required bool) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout, "resume")
			return 0
		}
	}
	args, dispatchOptions, dispatchErr := ParseDispatchOptions(args)
	if dispatchErr != nil {
		return fail(dispatchErr)
	}
	timeout := notifyDefaultTimeout
	var message, messageFile, agent, launcher, decision string
	var messageSet, agentSet, adopt, release, timeoutSet bool
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
			timeout, timeoutSet, i = n, true, next
		case strings.HasPrefix(arg, "--timeout="):
			n, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--timeout="), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(launchError("launch.resume_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout, timeoutSet = n, true
		case arg == "--adopt":
			adopt = true
		case arg == "--release":
			release = true
		case arg == "--decision":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "board.option_requires_a_value", arg)
			}
			decision, i = val, next
		case strings.HasPrefix(arg, "--decision="):
			decision = strings.TrimPrefix(arg, "--decision=")
		case arg == "--agent":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_agent_value")
			}
			if !config.ValidAgentName(val) {
				return usageFail("resume", "launch.unknown_agent", val)
			}
			agent, agentSet, i = val, true, next
		case strings.HasPrefix(arg, "--agent="):
			val := strings.TrimPrefix(arg, "--agent=")
			if !config.ValidAgentName(val) {
				return usageFail("resume", "launch.unknown_agent", val)
			}
			agent, agentSet = val, true
		case arg == "--launcher":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("resume", "launch.missing_launcher_value")
			}
			if !config.ValidLauncherName(val) {
				return usageFail("resume", "launch.unknown_launcher", val)
			}
			launcher, i = val, next
		case strings.HasPrefix(arg, "--launcher="):
			val := strings.TrimPrefix(arg, "--launcher=")
			if !config.ValidLauncherName(val) {
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
	if release && adopt {
		return usageFail("resume", "remote.release_takes_only_a_decision")
	}
	if release && (agentSet || messageSet || messageFile != "" || launcher != "" || timeoutSet || dispatchOptions != (DispatchOptions{}) || strings.TrimSpace(decision) == "") {
		return usageFail("resume", "remote.release_takes_only_a_decision")
	}
	if adopt && (agentSet || messageSet || messageFile != "" || launcher != "" || timeoutSet || dispatchOptions != (DispatchOptions{})) {
		return usageFail("resume", "launch.adopt_takes_only_a_decision")
	}
	if adopt && strings.TrimSpace(decision) == "" {
		return usageFail("resume", "board.adopt_decision_required", positional[0])
	}
	dispatchOptions.Decision = decision
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if required {
		if err := remote.RequireBinding(cfg); err != nil {
			return fail(err)
		}
	}
	if release {
		if !remote.ServiceSelected(cfg, required) {
			return fail(launchError("remote.release_requires_server"))
		}
		if err := commandRemoteRelease(cfg, positional[0], decision); err != nil {
			return fail(err)
		}
		return 0
	}
	if remote.ServiceSelected(cfg, required) && adopt {
		if err := commandRemoteAdopt(cfg, positional[0], decision); err != nil {
			return fail(err)
		}
		return 0
	}
	if !remote.ServiceSelected(cfg, required) && !adopt && !agentSet && decision != "" {
		return usageFail("resume", "launch.decision_needs_adopt_or_agent")
	}
	var agentPtr *string
	if agentSet {
		agentPtr = &agent
	}
	if remote.ServiceSelected(cfg, required) {
		return runRemoteResume(cfg, agentPtr, launcher, positional[0], message, messageFile, messageSet, timeout, dispatchOptions)
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	if adopt {
		if err := commandAdopt(root, positional[0], decision); err != nil {
			return fail(err)
		}
		return 0
	}
	if err := commandResume(root, agentPtr, launcher, positional[0], message, messageFile, messageSet, timeout, dispatchOptions); err != nil {
		return exitLaunch(err)
	}
	return 0
}
