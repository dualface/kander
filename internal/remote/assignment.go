package remote

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/probe"
)

// GitSubmitter reads the Git name in the current working directory. Keep
// whitespace in the configured value so the server can validate it.
func GitSubmitter(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := probe.CaptureWithEnvDirLimit(ctx, "git", []string{"config", "--null", "--get", "user.name"}, nil, "", 1<<20)
	if err != nil || res.Code != 0 || res.Overflow {
		return ""
	}
	return strings.TrimSuffix(res.Stdout, "\x00")
}

// NewAssignCommand builds kander assign.
func NewAssignCommand() *cobra.Command {
	var revision uint64
	c := command.RequireProject(&cobra.Command{
		Use:  "assign <task-id> [<assignee>]",
		Args: command.Args(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			var req api.Assign
			if len(args) == 2 {
				req.Assignee = args[1]
			}
			if c.Flags().Changed("expect-revision") {
				req.ExpectedRevision = &revision
			}
			return runAssign(args[0], req)
		},
	})
	c.Flags().Uint64Var(&revision, "expect-revision", 0, "")
	return c
}

func runAssign(id string, req api.Assign) error {
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	t, err := s.Project.Assign(operationCtx, client.NewKey(), id, req)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(assignResult{
			TaskID: t.TaskID, Assignee: board.MetadataFrom(t.Text, board.FieldAssignee), Revision: t.Revision,
		}))
	}
	_, err = fmt.Fprintln(stdout, t.Revision)
	return err
}

type assignResult struct {
	TaskID   string `json:"task_id"`
	Assignee string `json:"assignee"`
	Revision uint64 `json:"revision"`
}
