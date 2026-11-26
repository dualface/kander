package launch

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"unicode/utf8"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

func runRemoteIntegration(ctx context.Context, cfg *config.Config, args []string) int {
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(errors.New(config.Text("remote.not_configured")))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	if args[0] == "integration-show" {
		value, err := session.Project.Integration(ctx, api.IntegrationID(args[1]))
		if err != nil {
			return fail(err)
		}
		if code := command.Object(value); code != 0 {
			return code
		}
		return 0
	}
	file, err := os.Open(args[1])
	if err != nil {
		return fail(err)
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	err = errors.Join(err, file.Close())
	if err != nil {
		return fail(err)
	}
	if len(data) > 65536 || !utf8.Valid(data) {
		return command.FailStatus(2, command.Coded(command.CodeUsage, errors.New(t("remote.integration_input_invalid"))))
	}
	var options remote.IntegrationOptions
	if err := board.DecodeReviewJSON(data, &options); err != nil {
		return fail(err)
	}
	if err := cfg.Rules.CheckTaskGroup(options.GroupID); err != nil {
		return fail(err)
	}
	session.WorkerStarter = startRemoteWorker
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	value, err := session.Integrate(ctx, cwd, options)
	if err != nil {
		return fail(err)
	}
	if code := command.Object(value); code != 0 {
		return code
	}
	return 0
}
