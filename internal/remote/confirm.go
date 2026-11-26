package remote

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
)

// NewConfirmCommand builds kander confirm.
func NewConfirmCommand() *cobra.Command {
	var revision uint64
	c := command.RequireProject(&cobra.Command{
		Use:  "confirm <task-id>",
		Args: command.Args(1, 1),
		PreRunE: func(c *cobra.Command, _ []string) error {
			if err := command.Required(c, "expect-revision"); err != nil {
				return err
			}
			if revision == 0 {
				return command.Usage("remote.confirm_revision_required")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error { return runConfirm(args[0], revision) },
	})
	c.Flags().Uint64Var(&revision, "expect-revision", 0, "")
	return c
}

func runConfirm(id string, revision uint64) error {
	base, cancel := commandContext()
	defer cancel()
	actor := GitSubmitter(base)
	if strings.TrimSpace(actor) == "" {
		return fail(errors.New(config.Text("remote.confirm_git_name_required")))
	}
	s, _, err := open(base)
	if err != nil {
		return fail(err)
	}
	ctx, finish := s.OperationContext(base)
	defer finish()
	task, err := s.Project.Confirm(ctx, client.NewKey(), id, api.Confirm{ExpectedRevision: revision, ConfirmedBy: actor})
	if err != nil {
		return fail(err)
	}
	if !command.JSON() {
		return printTask(s, task)
	}
	confirmedBy, ok := board.LastConfirmer(task.Text)
	if !ok {
		return fail(errors.New(config.Text("remote.confirm_record_missing")))
	}
	return command.Exit(command.Result(struct {
		taskRef
		ConfirmedBy string `json:"confirmed_by"`
	}{newTaskRef(s, task), confirmedBy}))
}
