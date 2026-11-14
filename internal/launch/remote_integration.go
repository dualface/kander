package launch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
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
	if args[0] == "integration-show" {
		if !api.IntegrationTarget(args[1]) {
			return coordinatorUsageFail()
		}
		value, err := session.Project.Integration(ctx, api.IntegrationID(args[1]))
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			return fail(err)
		}
		return 0
	}
	if !filepath.IsAbs(args[1]) {
		return coordinatorUsageFail()
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
		return coordinatorUsageFail()
	}
	var options remote.IntegrationOptions
	if err := board.DecodeReviewJSON(data, &options); err != nil {
		return fail(err)
	}
	if err := cfg.Rules.CheckTaskGroup(options.GroupID); err != nil {
		return fail(err)
	}
	session.WorkerStarter = ensureRemoteWorker
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	value, err := session.Integrate(ctx, cwd, options)
	if err != nil {
		return fail(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		return fail(err)
	}
	return 0
}
