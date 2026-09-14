package issue

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// runResult exposes the controlled inspection, assessment and decision protocol.
// No raw gh write command is part of the agent workflow.
func runResult(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	values := map[string]string{"--action": "start"}
	number := 0
	for i := 0; i < len(args); i++ {
		if args[i] == "--help" || args[i] == "-h" {
			fmt.Fprintln(stdout, config.Text("issue.result_usage"))
			return 0
		}
		name := strings.SplitN(args[i], "=", 2)[0]
		switch name {
		case "--repo", "--card", "--agent", "--launcher", "--action", "--file":
			value, next, ok := optionValue(args, i, name, stderr)
			if !ok {
				return 2
			}
			i = next
			values[name] = value
		default:
			parsed, err := strconv.Atoi(args[i])
			if err != nil || parsed <= 0 || number != 0 {
				fmt.Fprintln(stderr, config.Text("issue.result_usage"))
				return 2
			}
			number = parsed
		}
	}
	if number == 0 || values["--card"] == "" {
		fmt.Fprintln(stderr, config.Text("issue.result_usage"))
		return 2
	}
	action := values["--action"]
	if action != "start" && action != "inspect" && action != "apply" && action != "decide" {
		fmt.Fprintln(stderr, config.Text("issue.result_usage"))
		return 2
	}
	provider := factory()
	repository, code := resolveRepository(provider, values["--repo"], values["--repo"] != "", stderr)
	if code != 0 {
		return code
	}
	root, err := board.BoardRoot()
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	writer, ok := provider.(ResultProvider)
	if !ok {
		fmt.Fprintln(stderr, formatTriageError(resultError("result provider unavailable")))
		return 1
	}
	ctx, cancel := commandContext()
	defer cancel()
	var out any
	switch action {
	case "start":
		out, err = StartResult(ctx, writer, root, repository, number, TriageOptions{CardID: values["--card"], Agent: values["--agent"], Launcher: values["--launcher"]})
	case "inspect":
		out, err = InspectResult(ctx, writer, root, repository, number, values["--card"])
	case "apply", "decide":
		var data []byte
		data, err = os.ReadFile(values["--file"])
		if err == nil {
			if len(data) > 65536 {
				err = resultError("input exceeds 64 KiB")
			} else if action == "apply" {
				var proposal ResultProposal
				err = json.Unmarshal(data, &proposal)
				if err == nil {
					out, err = ApplyResult(ctx, writer, root, repository, number, values["--card"], proposal)
				}
			} else {
				var decision ResultDecision
				err = json.Unmarshal(data, &decision)
				if err == nil {
					out, err = DecideResult(ctx, writer, root, repository, number, values["--card"], decision)
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	return 0
}
