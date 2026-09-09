package review

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// Run is the entry point of the `kander review` subcommand. args excludes the program name and the subcommand name.
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
		return runDispositionCommand(args)
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
	if agent == "" {
		roleInput := rest[3]
		roles := map[string]string{
			"pm": "PM", "qa": "QA", "csa": "CSA",
			"codesecurityanalyst": "CSA", "hacker": "Hacker",
		}
		role := roles[toLower(roleInput)]
		if role == "" {
			userError(config.Text("review.unsupported_role", roleInput))
			return 2
		}
		agent, err = reviewerFromConfig(role)
		if err != nil {
			userError(err.Error())
			return 1
		}
	}
	archiveRoot := ""
	replay := false
	if len(options.tasks) > 0 {
		archiveRoot, err = board.BoardRootAt(rest[0])
		if err != nil {
			userError(err.Error())
			return 2
		}
		unlock, e := board.LockReviewRun(archiveRoot, options.runID)
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
		old, exists, e := board.LookupReviewRun(archiveRoot, options.runID)
		if e != nil {
			userError(e.Error())
			return 2
		}
		if exists && !sameReplayTarget(old, options) {
			userError(config.Text("review.archive_invalid", "run identity conflict"))
			return 2
		}
		replay = exists
	}
	if len(options.tasks) > 0 {
		rest, err = hydrateIncremental(archiveRoot, options, rest, replay)
		if err != nil {
			userError(err.Error())
			return 2
		}
	}
	ctx, err := validateContextMode(agent, rest, replay)
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
		fresh, e := archiveInvocation(&ctx, options, rest, archiveRoot)
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

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
