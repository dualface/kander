package issue

import (
	"context"
	"os"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// The commands that need a board reach it through these entrances. The client
// binds them to the server session of the working directory; tests rebind them
// to a board directory, so the command front end stays under test without a
// server.
var (
	importCard      = importThroughServer
	startTriage     = triageThroughServer
	openResultBoard = openResultBoardThroughServer
)

func importThroughServer(ctx context.Context, provider IssueProvider, repository Repository, number int, options ImportOptions) (ImportResult, error) {
	session, _, finish, err := openServerSession(ctx, true)
	if err != nil {
		return ImportResult{}, err
	}
	defer finish()
	if options.languageFromConfig {
		// The configuration read before the session may predate the project policy the
		// session applied; an explicit --language always stays.
		options.Language = session.cfg.AgentLanguage
	}
	return ImportRemote(session.ctx, session.session, provider, repository, number, options)
}

func triageThroughServer(ctx context.Context, provider IssueProvider, repository Repository, number int, options TriageOptions) (TriageOutcome, error) {
	session, cwd, finish, err := openServerSession(ctx, true)
	if err != nil {
		return TriageOutcome{}, err
	}
	defer finish()
	return StartRemoteTriage(session.ctx, session.session, provider, remote.ProjectDir(cwd), repository, number, options, false)
}

// openResultBoardThroughServer returns the context and data root the result
// commands work in, and the function that ends the operation.
func openResultBoardThroughServer(ctx context.Context) (context.Context, string, func(), error) {
	session, cwd, finish, err := openServerSession(ctx, false)
	if err != nil {
		return ctx, "", func() {}, err
	}
	root, err := session.session.LocalDataRoot()
	if err != nil {
		finish()
		return ctx, "", func() {}, err
	}
	return WithRemoteResult(session.ctx, session.session, cwd), root, finish, nil
}

type serverSession struct {
	ctx     context.Context
	session *remote.Session
	// cfg is the configuration the session opened with, the project policy applied.
	cfg *config.Config
}

// openServerSession binds the working directory's project, refusing a client
// without a project binding, and starts one bounded operation on it. missingOK
// is the argument config.Load takes: the import and triage commands accept a
// missing configuration file, the result commands do not.
func openServerSession(ctx context.Context, missingOK bool) (serverSession, string, func(), error) {
	cfg, err := config.Load(missingOK)
	if err != nil {
		return serverSession{}, "", nil, err
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return serverSession{}, "", nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return serverSession{}, "", nil, err
	}
	session, err := remote.Open(ctx, cfg, cwd)
	if err != nil {
		return serverSession{}, "", nil, err
	}
	operationCtx, finish := session.OperationContext(ctx)
	return serverSession{ctx: operationCtx, session: session, cfg: cfg}, cwd, finish, nil
}
