package issue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// runResult exposes the controlled inspection, assessment and decision protocol.
// No raw gh write command is part of the agent workflow.
func runResult(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	return runResultMode(factory, args, stdout, stderr, false)
}

func runResultMode(factory func() IssueProvider, args []string, stdout, stderr io.Writer, required bool) int {
	values := map[string]string{"--action": "start"}
	number := 0
	for i := 0; i < len(args); i++ {
		if args[i] == "--help" || args[i] == "-h" {
			fmt.Fprintln(stdout, usageText())
			return 0
		}
		name := strings.SplitN(args[i], "=", 2)[0]
		switch name {
		case "--repo", "--card", "--agent", "--launcher", "--action", "--file", "--operation-id":
			value, next, ok := optionValue(args, i, name, stderr)
			if !ok {
				return 2
			}
			i = next
			values[name] = value
		default:
			parsed, err := strconv.Atoi(args[i])
			if err != nil || parsed <= 0 || number != 0 {
				fmt.Fprintln(stderr, usageText())
				return 2
			}
			number = parsed
		}
	}
	if number == 0 || values["--card"] == "" {
		fmt.Fprintln(stderr, usageText())
		return 2
	}
	action := values["--action"]
	if action != "start" && action != "inspect" && action != "apply" && action != "decide" {
		fmt.Fprintln(stderr, usageText())
		return 2
	}
	if operation := values["--operation-id"]; operation != "" && (action != "start" || !api.ValidIdentity(operation)) {
		fmt.Fprintln(stderr, usageText())
		return 2
	}
	provider := factory()
	repository, code := resolveRepository(provider, values["--repo"], values["--repo"] != "", stderr)
	if code != 0 {
		return code
	}
	writer, ok := provider.(ResultProvider)
	if !ok {
		fmt.Fprintln(stderr, formatTriageError(resultError("result provider unavailable")))
		return 1
	}
	ctx, cancel := commandContext()
	defer cancel()
	cfg, err := config.Load(false)
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	root := ""
	if required {
		if err := remote.RequireBinding(cfg); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
	}
	if remote.ServiceSelected(cfg, required) {
		cwd, e := os.Getwd()
		if e != nil {
			err = e
		} else {
			session, e := remote.Open(ctx, cfg, cwd)
			if e != nil {
				err = e
			} else {
				operationCtx, finishOperation := session.OperationContext(ctx)
				defer finishOperation()
				ctx = operationCtx
				root, err = session.LocalDataRoot()
				ctx = WithRemoteResult(ctx, session, cwd)
			}
		}
	} else {
		root, err = board.BoardRoot()
	}
	if err != nil {
		fmt.Fprintln(stderr, formatTriageError(err))
		return 1
	}
	var out any
	switch action {
	case "start":
		out, err = StartResult(ctx, writer, root, repository, number, TriageOptions{OperationID: values["--operation-id"], CardID: values["--card"], Agent: values["--agent"], Launcher: values["--launcher"]})
	case "inspect":
		out, err = InspectResult(ctx, writer, root, repository, number, values["--card"])
	case "apply", "decide":
		var data []byte
		var file *os.File
		file, err = os.Open(values["--file"])
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(file, 65537))
			err = errors.Join(err, file.Close())
		}
		if err == nil {
			if len(data) > 65536 || !utf8.Valid(data) {
				err = resultError("input exceeds 64 KiB")
			} else if action == "apply" {
				var proposal ResultProposal
				err = board.DecodeReviewJSON(data, &proposal)
				if err == nil {
					out, err = ApplyResult(ctx, writer, root, repository, number, values["--card"], proposal)
				}
			} else {
				var decision ResultDecision
				err = board.DecodeReviewJSON(data, &decision)
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
