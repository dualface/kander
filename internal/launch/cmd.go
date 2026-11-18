package launch

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	cli.Register(NewStartCommand)
	cli.Register(NewResumeCommand)
	cli.Register(NewDispatchCommand)
	cli.Register(NewCoordinatorCommand)
	cli.Register(NewOrchestrateCommand)
	// The issue command front end never imports this package; the takeover
	// starter is wired here so every binary that can start agents also supports
	// `kander issue triage` and the TUI's `s` confirmation.
	issue.SetTriageStarter(StartTriage)
	issue.SetLauncherGate(RefuseTerminalLauncher)
}

func fail(err error) int { return command.Fail(err) }

// AddAgentFlags registers --agent and --launcher, which name a configured execution agent and
// a launcher.
func AddAgentFlags(c *cobra.Command, agent, launcher *string) {
	command.Checked(c.Flags(), agent, "agent", "", func(name string) error {
		if !config.ValidAgentName(name) {
			return command.Usage("launch.unknown_agent", name)
		}
		return nil
	})
	command.Checked(c.Flags(), launcher, "launcher", "", func(name string) error {
		if !config.ValidLauncherName(name) {
			return command.Usage("launch.unknown_launcher", name)
		}
		return nil
	})
}

// AddDispatchFlags registers the flags that opt a notify or resume message into the durable
// dispatch protocol. Each may be given once and never empty.
func AddDispatchFlags(c *cobra.Command, o *DispatchOptions) {
	command.OnceNonEmpty(c.Flags(), &o.ID, "dispatch-id", "")
	command.OnceNonEmpty(c.Flags(), &o.Kind, "kind", "")
	command.OnceNonEmpty(c.Flags(), &o.Base, "base", "")
	command.OnceNonEmpty(c.Flags(), &o.EvidenceFile, "evidence-file", "")
}

// NewStartCommand builds kander start.
func NewStartCommand() *cobra.Command {
	var agent, launcher string
	c := &cobra.Command{
		Use:  "start [task-id]",
		Args: command.Args(0, 1),
		RunE: func(_ *cobra.Command, args []string) error { return command.Exit(runStart(agent, launcher, args)) },
	}
	AddAgentFlags(c, &agent, &launcher)
	return c
}

// NewResumeCommand builds kander resume.
func NewResumeCommand() *cobra.Command {
	var o resumeOptions
	c := &cobra.Command{
		Use:  "resume <task-id>",
		Args: command.Args(1, 1),
	}
	c.PreRunE = func(_ *cobra.Command, args []string) error { return o.validate(c, args[0]) }
	c.RunE = func(_ *cobra.Command, args []string) error { return command.Exit(runResume(c, args[0], o)) }
	flags := c.Flags()
	flags.StringVar(&o.message, "message", "", "")
	flags.StringVar(&o.messageFile, "message-file", "", "")
	command.Seconds(flags, &o.timeout, "timeout", notifyDefaultTimeout, "", "launch.resume_timeout_must_be_a_finite_number_greater_than")
	flags.BoolVar(&o.adopt, "adopt", false, "")
	flags.BoolVar(&o.release, "release", false, "")
	flags.StringVar(&o.decision, "decision", "", "")
	AddAgentFlags(c, &o.agent, &o.launcher)
	AddDispatchFlags(c, &o.dispatch)
	return c
}

// resumeOptions are the flags of kander resume.
type resumeOptions struct {
	message, messageFile, decision, agent, launcher string
	timeout                                         float64
	adopt, release                                  bool
	dispatch                                        DispatchOptions
}

// validate applies the contract's rules for the three forms of resume: a message, --adopt, and
// --release, the last two of which take only a decision.
func (o resumeOptions) validate(c *cobra.Command, task string) error {
	if o.release && o.adopt {
		return command.Usage("remote.release_takes_only_a_decision")
	}
	if !o.release && !o.adopt {
		return nil
	}
	for _, name := range []string{"agent", "message", "message-file", "launcher", "timeout", "dispatch-id", "kind", "base", "evidence-file"} {
		if c.Flags().Changed(name) {
			if o.release {
				return command.Usage("remote.release_takes_only_a_decision")
			}
			return command.Usage("launch.adopt_takes_only_a_decision")
		}
	}
	switch {
	case strings.TrimSpace(o.decision) == "" && o.release:
		return command.Usage("remote.release_takes_only_a_decision")
	case strings.TrimSpace(o.decision) == "":
		return command.Usage("board.adopt_decision_required", task)
	}
	return nil
}

// runStart implements kander start in server mode.
func runStart(agent, launcher string, rest []string) int {
	var taskID string
	if len(rest) == 0 && command.JSON() {
		return command.FailStatus(2, command.Interactive(t("launch.interactive_select_task")))
	}
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
		command.Notify("launch_warning", warning)
	}
	if err != nil {
		return exitLaunch(err)
	}
	if result.Superseded {
		return supersededExit("start", result.TaskID, result.Plan, result.Outcome)
	}
	return startedExit("start", board.Entry{TaskID: result.TaskID, Kind: result.Size}, result.Agent, result.Plan, result.Outcome, nil)
}

// runResume implements kander resume against the server.
func runResume(c *cobra.Command, task string, o resumeOptions) int {
	dispatchOptions := o.dispatch
	dispatchOptions.Decision = o.decision
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	if o.release {
		if err := commandRemoteRelease(cfg, task, o.decision); err != nil {
			return fail(err)
		}
		return resumeActionExit("release", task)
	}
	if o.adopt {
		if err := commandRemoteAdopt(cfg, task, o.decision); err != nil {
			return fail(err)
		}
		return resumeActionExit("adopt", task)
	}
	var agentPtr *string
	if c.Flags().Changed("agent") {
		agentPtr = &o.agent
	}
	return runRemoteResume(cfg, agentPtr, o.launcher, task, o.message, o.messageFile, c.Flags().Changed("message"), o.timeout, dispatchOptions)
}

// resumeActionExit ends a resume that only released or adopted the execution of a card.
func resumeActionExit(action, task string) int {
	if command.JSON() {
		return command.Result(struct {
			Action string `json:"action"`
			TaskID string `json:"task_id"`
		}{action, task})
	}
	return 0
}
