package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// openReviewStore returns the store of the board that holds the tasks of a
// review run; the commits are the range the run reviews. The client binds it
// to the server project of the checkout; tests bind it to a board directory.
var openReviewStore = openServerReviewStore

func openServerReviewStore(cfg *config.Config, cwd string, commits ...string) (reviewStore, error) {
	if err := remote.RequireBinding(cfg); err != nil {
		return nil, err
	}
	session, err := remote.Open(context.Background(), cfg, cwd)
	if err != nil {
		return nil, err
	}
	if session.Info.Remote != "" {
		for _, commit := range commits {
			if err := session.FetchCommit(context.Background(), cwd, commit); err != nil {
				return nil, err
			}
		}
	}
	return newRemoteReviewStore(session)
}

// Run is the entry point of the `kander review` subcommand. args excludes the program name and the subcommand name.
// Every board-backed form goes through the server.
func Run(args []string) (exitCode int) {
	if len(args) > 0 && args[0] == windowsJobBootstrap {
		return windowsJobBootstrapMain(args[1:])
	}
	applyUmask()
	config.BindEffectiveLanguage()
	if len(args) == 0 {
		usage()
		return 2
	}
	if _, err := config.Load(false); err != nil {
		userError(err.Error())
		return 1
	}
	if dispositionCommand(args[0]) {
		return runDispositionMode(args)
	}
	options, args, err := parseArchiveOptions(args)
	if err != nil {
		userError(err.Error())
		return 2
	}
	agent, rest, err := splitAgentArgs(args)
	if err != nil {
		if errors.Is(err, errUsage) {
			usage()
			return 2
		}
		var ge *gateError
		if errors.As(err, &ge) {
			if ge.message != "" {
				userError(ge.message)
			} else {
				usage()
			}
			return ge.code
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(rest) < 5 || len(rest) > 7 {
		usage()
		return 2
	}
	archiveRoot := ""
	var stores []reviewStore
	replay := false
	var existing board.ReviewRun
	if len(options.tasks) > 0 {
		cfg, loadErr := config.Effective(nil)
		if loadErr != nil {
			userError(loadErr.Error())
			return 2
		}
		store, err := openReviewStore(cfg, rest[0], rest[1], rest[2])
		if err != nil {
			userError(err.Error())
			return 2
		}
		archiveRoot = store.Root()
		stores = []reviewStore{store}
		unlock, e := store.Lock(options.runID)
		if e != nil {
			userError(e.Error())
			return 2
		}
		defer func() {
			if e := unlock(); e != nil {
				exitCode = 2
				userError(e.Error())
			}
		}()
		old, exists, e := store.Lookup(options.runID)
		if e != nil {
			userError(e.Error())
			return 2
		}
		if exists && !sameReplayTarget(old, options) {
			userError(config.Text("review.archive_invalid", "run identity conflict"))
			return 2
		}
		replay = exists
		existing = old
	}
	role, ok := parseReviewRole(rest[3])
	if !ok || !admitReviewRole(role, archiveRoot, options, replay, existing, stores...) {
		userError(config.Text("review.unsupported_role", rest[3]))
		return 2
	}
	rest[3] = role
	pinRole := reviewerConfigRole(role)
	var pinning reviewPinning
	if len(options.tasks) > 0 {
		pinning, err = boundReviewPinning(archiveRoot, options.tasks, pinRole, stores...)
		if err != nil {
			userError(err.Error())
			return 2
		}
	}
	scale := pinning.scale
	if scale == "" {
		scale = "large"
		if len(rest) >= 5 {
			scale = reviewScaleFromTask(rest[4])
		}
	}
	agent, agentSource, err := pinnedReviewer(pinning, pinRole, agent)
	if err != nil {
		userError(err.Error())
		return 2
	}
	if agent == "" {
		agent, err = reviewerFromConfig(pinRole, scale)
		if err != nil {
			userError(err.Error())
			return 1
		}
	}
	if len(options.tasks) > 0 {
		rest, err = hydrateIncremental(archiveRoot, options, rest, replay, stores...)
		if err != nil {
			userError(err.Error())
			return 2
		}
	}
	ctx, err := validateContextMode(agent, rest, replay, pinning.scale)
	if err == nil {
		ctx.resolved, err = applyReviewPin(&ctx, pinning, pinRole, agentSource, scale)
	}
	if err != nil {
		var ge *gateError
		if errors.As(err, &ge) {
			if ge.message != "" {
				userError(ge.message)
			}
			return ge.code
		}
		userError(err.Error())
		return 2
	}
	if len(options.tasks) > 0 {
		findingsSchema := board.FindingsSchemaReceiver
		if replay {
			findingsSchema = existing.FindingsSchema
		}
		fresh, e := archiveInvocation(&ctx, options, rest, archiveRoot, findingsSchema, stores...)
		if e != nil {
			userError(e.Error())
			return 2
		}
		if !fresh {
			return ctx.archive.recover()
		}
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	return executeReview(ctx, interrupt)
}
