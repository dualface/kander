package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// NewGuardWriteCommand builds kander guard-write, an advisory guard for client-side writes.
// Formal task documents are published through REST; KANBAN_DIR never selects a write root.
func NewGuardWriteCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "guard-write <path>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error { return runGuardWrite(args[0]) },
	}
}

func runGuardWrite(target string) error {
	ctx, cancel := commandContext()
	defer cancel()
	session, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	path, err := filepath.Abs(target)
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	legacy := filepath.Join(ProjectDir(cwd), "kanban")
	rel, err := filepath.Rel(legacy, path)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fail(errors.New(config.Text("remote.guard_service_documents")))
	}
	// A managed task process is fixed to one execution. This advisory check
	// cannot prevent arbitrary filesystem writes, but never borrows a successor.
	execution := os.Getenv(EnvExecutionID)
	if execution != "" {
		credentials, err := Credentials(session.Home)
		if err != nil {
			return fail(err)
		}
		for _, credential := range credentials {
			if credential.ExecutionID == execution && credential.ResourceKind == "" && credential.ProjectID == session.Info.ID && credential.ServerURL == session.ServerURL && credential.ClientID == session.Client.ClientID() {
				if err := session.checkExecution(ctx, credential); err != nil {
					return fail(err)
				}
				return nil
			}
		}
		// Board-free sessions have no task credential. This permits local code
		// inspection only; every formal mutation still requires service authority.
	}
	return nil
}
