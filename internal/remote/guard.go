package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// RunGuardWrite is an advisory guard for client-side writes. Formal task
// documents are published through REST; KANBAN_DIR never selects a write root.
func RunGuardWrite(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return printUsage("guard-write")
	}
	ctx, cancel := commandContext()
	defer cancel()
	session, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	path, err := filepath.Abs(args[0])
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
				return 0
			}
		}
		// Board-free sessions have no task credential. This permits local code
		// inspection only; every formal mutation still requires service authority.
	}
	return 0
}
