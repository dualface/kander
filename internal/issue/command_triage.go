package issue

import (
	"fmt"
	"github.com/dualface/kander/internal/remote"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
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

// runTriage implements `kander issue triage NUMBER`. The command prepares the
// evidence and starts the takeover session through the shared StartTriage path,
// so the TUI and the CLI always launch the same way.
func runTriage(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	return runTriageMode(factory, args, stdout, stderr, false)
}

func runTriageMode(factory func() IssueProvider, args []string, stdout, stderr io.Writer, required bool) int {
	var options triageCommandOptions
	positional := 0
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, usageText())
			return 0
		case matchesLongOption(arg, "--repo"), matchesLongOption(arg, "--card"),
			matchesLongOption(arg, "--agent"), matchesLongOption(arg, "--launcher"):
			name := arg
			if eq := strings.Index(name, "="); eq >= 0 {
				name = name[:eq]
			}
			value, next, ok := optionValue(args, index, name, stderr)
			if !ok {
				return 2
			}
			index = next
			switch name {
			case "--repo":
				options.repository = value
				options.hasRepo = true
			case "--card":
				options.card = value
			case "--agent":
				options.agent = value
			case "--launcher":
				options.launcher = value
			}
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
			fmt.Fprintln(stderr, usageText())
			return 2
		default:
			positional++
			if positional > 1 {
				fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
				fmt.Fprintln(stderr, usageText())
				return 2
			}
			number, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil || number <= 0 {
				fmt.Fprintln(stderr, config.Text("issue.error_invalid_value", "NUMBER", arg))
				return 2
			}
			options.number = number
		}
	}
	if positional == 0 {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "NUMBER"))
		fmt.Fprintln(stderr, usageText())
		return 2
	}
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}

	ctx, cancel := commandContext()
	defer cancel()
	cfg, err := config.Load(true)
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	triageOptions := TriageOptions{CardID: options.card, Agent: options.agent, Launcher: options.launcher}
	var outcome TriageOutcome
	if required {
		if err := remote.RequireBinding(cfg); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
	}
	if remote.ServiceSelected(cfg, required) {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			fmt.Fprintln(stderr, formatTriageError(cwdErr))
			return 1
		}
		session, openErr := remote.Open(ctx, cfg, cwd)
		if openErr != nil {
			fmt.Fprintln(stderr, formatTriageError(openErr))
			return 1
		}
		outcome, err = StartRemoteTriage(ctx, session, provider, remote.ProjectDir(cwd), repository, options.number, triageOptions, false)
	} else {
		var root string
		root, err = board.BoardRoot()
		if err == nil {
			outcome, err = StartTriage(ctx, provider, root, repository, options.number, triageOptions)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	identity := triageIdentity(repository, options.number)
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

// triageIdentity renders the confirmed identity of one takeover target. It is
// built from the validated repository fields and the issue number only.
func triageIdentity(repository Repository, number int) string {
	return repository.Owner + "/" + repository.Name + "#" + strconv.Itoa(number)
}

func formatTriageError(err error) string {
	return "kander issue: " + TriageError(err)
}
