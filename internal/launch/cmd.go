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
	cli.Commands["start"] = RunStartRemote
	cli.Commands["resume"] = cli.RequireService(RunResumeRemote)
	cli.Commands["dispatch"] = cli.RequireService(RunDispatchRemote)
	cli.Commands["coordinator"] = func(args []string) int {
		if _, status, refused := coordinatorArgs(args); refused {
			return status
		}
		return cli.RequireService(RunCoordinatorRemote)(args)
	}
	cli.Commands["orchestrate"] = cli.RequireService(RunOrchestrateRemote)
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
		operationCtx, finishOperation := session.OperationContext(ctx)
		defer finishOperation()
		ctx = operationCtx
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

// RunResumeRemote implements kander resume against the server.
func RunResumeRemote(args []string) int {
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
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	if release {
		if err := commandRemoteRelease(cfg, positional[0], decision); err != nil {
			return fail(err)
		}
		return 0
	}
	if adopt {
		if err := commandRemoteAdopt(cfg, positional[0], decision); err != nil {
			return fail(err)
		}
		return 0
	}
	var agentPtr *string
	if agentSet {
		agentPtr = &agent
	}
	return runRemoteResume(cfg, agentPtr, launcher, positional[0], message, messageFile, messageSet, timeout, dispatchOptions)
}
