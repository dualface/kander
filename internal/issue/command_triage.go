package issue

import (
	"fmt"
	"io"
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
	var options triageCommandOptions
	positional := 0
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, config.Text("issue.triage_usage"))
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
			fmt.Fprintln(stderr, config.Text("issue.triage_usage"))
			return 2
		default:
			positional++
			if positional > 1 {
				fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
				fmt.Fprintln(stderr, config.Text("issue.triage_usage"))
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
		fmt.Fprintln(stderr, config.Text("issue.triage_usage"))
		return 2
	}
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	root, err := board.BoardRoot()
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	ctx, cancel := commandContext()
	defer cancel()
	outcome, err := StartTriage(ctx, provider, root, repository, options.number, TriageOptions{
		CardID:   options.card,
		Agent:    options.agent,
		Launcher: options.launcher,
	})
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
