package issue

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
)

type resultOptions struct {
	repository, card, agent, launcher, action, file, operationID string
	hasRepo                                                      bool
	number                                                       int
}

func newResultCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	options := resultOptions{action: "start"}
	c := &cobra.Command{Use: "result NUMBER", Args: numberArgs}
	c.PreRunE = func(*cobra.Command, []string) error {
		if err := command.Required(c, "card"); err != nil {
			return err
		}
		if c.Flags().Changed("operation-id") && options.action != "start" {
			return command.Usage("cli.error_flag_needs", "--operation-id", "--action start")
		}
		return nil
	}
	c.RunE = func(_ *cobra.Command, args []string) error {
		options.hasRepo, options.number = c.Flags().Changed("repo"), issueNumber(args)
		return command.Exit(runResult(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	command.Checked(c.Flags(), &options.card, "card", "", nonBlank("card"))
	command.Checked(c.Flags(), &options.agent, "agent", "", nonBlank("agent"))
	command.Checked(c.Flags(), &options.launcher, "launcher", "", nonBlank("launcher"))
	command.Checked(c.Flags(), &options.action, "action", "", func(value string) error {
		if value != "start" && value != "inspect" && value != "apply" && value != "decide" {
			return command.Usage("issue.error_invalid_value", "--action", value)
		}
		return nil
	})
	command.Checked(c.Flags(), &options.file, "file", "", nonBlank("file"))
	command.Checked(c.Flags(), &options.operationID, "operation-id", "", func(value string) error {
		if !api.ValidIdentity(value) {
			return command.Usage("issue.error_invalid_value", "--operation-id", value)
		}
		return nil
	})
	return c
}

// runResult exposes the controlled inspection, assessment and decision protocol.
// No raw gh write command is part of the agent workflow.
func runResult(factory func() IssueProvider, options resultOptions, stdout, stderr io.Writer) int {
	action, number := options.action, options.number
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	writer, ok := provider.(ResultProvider)
	if !ok {
		return failTriage(stderr, resultError("result provider unavailable"), 1)
	}
	ctx, cancel := commandContext()
	defer cancel()
	ctx, root, finishOperation, err := openResultBoard(ctx)
	defer finishOperation()
	if err != nil {
		return failTriage(stderr, err, 1)
	}
	var out any
	switch action {
	case "start":
		out, err = StartResult(ctx, writer, root, repository, number, TriageOptions{OperationID: options.operationID, CardID: options.card, Agent: options.agent, Launcher: options.launcher})
	case "inspect":
		out, err = InspectResult(ctx, writer, root, repository, number, options.card)
	case "apply", "decide":
		var data []byte
		var file *os.File
		file, err = os.Open(options.file)
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
					out, err = ApplyResult(ctx, writer, root, repository, number, options.card, proposal)
				}
			} else {
				var decision ResultDecision
				err = board.DecodeReviewJSON(data, &decision)
				if err == nil {
					out, err = DecideResult(ctx, writer, root, repository, number, options.card, decision)
				}
			}
		}
	}
	if err != nil {
		return failTriage(stderr, err, 1)
	}
	if command.JSON() {
		return command.Result(out)
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return failTriage(stderr, err, 1)
	}
	return 0
}
