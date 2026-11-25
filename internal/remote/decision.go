package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

const maxDecisionInput = 1 << 20

// NewDecisionCommand builds kander decision.
func NewDecisionCommand() *cobra.Command {
	var kind, file string
	record := &cobra.Command{
		Use:  "record <task-id>",
		Args: command.Args(1, 1),
		PreRunE: func(c *cobra.Command, _ []string) error {
			if err := command.Required(c, "kind", "file"); err != nil {
				return err
			}
			if !api.DecisionKind(kind).Valid() {
				return command.Usage("cli.error_invalid_value", "--kind", command.Escape(kind))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runDecisionRecord(args[0], api.DecisionKind(kind), file)
		},
	}
	command.OnceNonEmpty(record.Flags(), &kind, "kind", "")
	command.OnceNonEmpty(record.Flags(), &file, "file", "")
	list := &cobra.Command{
		Use:  "list <task-id>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error { return runDecisionList(args[0]) },
	}
	mode := &cobra.Command{
		Use: "mode <task-id> <leader|assignee>",
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(2, 2)(c, args); err != nil {
				return err
			}
			if !api.DecisionMode(args[1]).Valid() {
				return command.Usage("cli.error_invalid_value", "<mode>", command.Escape(args[1]))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runDecisionMode(args[0], api.DecisionMode(args[1]))
		},
	}
	return command.Group("decision", command.RequireProject(record), command.RequireProject(list), command.RequireProject(mode))
}

func readDecisionInput(path string) (api.DecisionInput, error) {
	var zero api.DecisionInput
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxDecisionInput+1))
	if err != nil {
		return zero, err
	}
	if len(data) > maxDecisionInput || !utf8.Valid(data) {
		return zero, errors.New(config.Text("remote.decision_input", maxDecisionInput))
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var input *api.DecisionInput
	if err := d.Decode(&input); err != nil || input == nil {
		return zero, errors.New(config.Text("remote.decision_input", maxDecisionInput))
	}
	if _, err := d.Token(); err != io.EOF {
		return zero, errors.New(config.Text("remote.decision_input", maxDecisionInput))
	}
	return *input, nil
}

func runDecisionRecord(id string, kind api.DecisionKind, path string) error {
	input, err := readDecisionInput(path)
	if err != nil {
		return fail(err)
	}
	ctx, s, release, err := openOperationSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	if input.RecordedBy == "" {
		input.RecordedBy = GitSubmitter(ctx)
	}
	record, err := s.Project.RecordDecision(ctx, client.NewKey(), id, kind, input)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(record))
	}
	_, err = fmt.Fprintln(stdout, record.ID)
	return err
}

func runDecisionList(id string) error {
	ctx, s, release, err := openOperationSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	list, err := s.Project.Decisions(ctx, id)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(list))
	}
	for _, r := range list.Decisions {
		if _, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.CreatedAt.Format(time.RFC3339Nano),
			command.Escape(r.RecordedBy), command.Escape(r.Question), command.Escape(r.Answer)); err != nil {
			return err
		}
	}
	return nil
}

func runDecisionMode(id string, mode api.DecisionMode) error {
	ctx, s, release, err := openOperationSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	changed, err := s.Project.SetDecisionMode(ctx, client.NewKey(), id, api.SetDecisionMode{Mode: mode})
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(changed))
	}
	_, err = fmt.Fprintln(stdout, changed.Revision)
	return err
}
