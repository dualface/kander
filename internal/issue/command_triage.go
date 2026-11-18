package issue

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

type triageCommandOptions struct {
	repository string
	hasRepo    bool
	number     int
	card       string
	agent      string
	launcher   string
}

func newTriageCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	var options triageCommandOptions
	c := &cobra.Command{Use: "triage NUMBER", Args: numberArgs}
	c.RunE = func(_ *cobra.Command, args []string) error {
		options.hasRepo, options.number = c.Flags().Changed("repo"), issueNumber(args)
		return command.Exit(runTriage(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	command.Checked(c.Flags(), &options.card, "card", "", nonBlank("card"))
	command.Checked(c.Flags(), &options.agent, "agent", "", nonBlank("agent"))
	command.Checked(c.Flags(), &options.launcher, "launcher", "", nonBlank("launcher"))
	return c
}

// runTriage implements `kander issue triage NUMBER`. The command prepares the
// evidence and starts the takeover session through the shared StartTriage path,
// so the TUI and the CLI always launch the same way.
func runTriage(factory func() IssueProvider, options triageCommandOptions, stdout, stderr io.Writer) int {
	// The session always starts: refuse a launcher that takes the terminal before anything,
	// the server session and its client state included, is opened.
	if err := gateLauncher(options.launcher); err != nil {
		return failTriage(stderr, err, 1)
	}
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}

	ctx, cancel := commandContext()
	defer cancel()
	triageOptions := TriageOptions{CardID: options.card, Agent: options.agent, Launcher: options.launcher}
	outcome, err := startTriage(ctx, provider, repository, options.number, triageOptions)
	if err != nil {
		return failTriage(stderr, err, 1)
	}
	identity := triageIdentity(repository, options.number)
	if command.JSON() {
		for _, warning := range outcome.Warnings {
			command.Notify("launch_warning", warning)
		}
		return command.Result(triageView{Agent: outcome.Agent, Launcher: outcome.Launcher, Issue: identity, Address: outcome.Address})
	}
	if outcome.Address == "" {
		fmt.Fprintln(stdout, config.Text("issue.triage_started_no_address", outcome.Agent, outcome.Launcher, identity))
	} else {
		fmt.Fprintln(stdout, config.Text("issue.triage_started", outcome.Agent, outcome.Launcher, identity, outcome.Address))
	}
	for _, warning := range outcome.Warnings {
		fmt.Fprintln(stdout, warning)
	}
	return 0
}

// triageView is the result object of kander issue triage.
type triageView struct {
	Agent    string `json:"agent"`
	Launcher string `json:"launcher"`
	Issue    string `json:"issue"`
	Address  string `json:"address"`
}

// triageIdentity renders the confirmed identity of one takeover target. It is
// built from the validated repository fields and the issue number only.
func triageIdentity(repository Repository, number int) string {
	return repository.Owner + "/" + repository.Name + "#" + strconv.Itoa(number)
}

func formatTriageError(err error) string {
	return "kander issue: " + TriageError(err)
}
