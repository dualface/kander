package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/menu"
)

func init() {
	// Without a subcommand the board opens directly, and the whole UI happens inside the alt-screen.
	cli.DefaultRunner = Run
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

// configuredAgentLanguage returns the agent language the import freezes into a
// new card when the caller does not pass one; the issue service validates the
// value, so an empty result is reported as an invalid import option.
func configuredAgentLanguage() string {
	cfg, err := config.Load(false)
	if err != nil {
		return ""
	}
	return cfg.AgentLanguage
}

// runBoardTUI starts the interactive board; tests may override it to inspect the App without Bubble Tea.
var runBoardTUI = runTUI

// isInteractiveTerminal reports whether stdin/stdout are TTYs; tests may override it.
var isInteractiveTerminal = func() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func requireTerminal() error {
	if isInteractiveTerminal() {
		return nil
	}
	return errors.New(t(
		"tui.tui_requires_an_interactive_terminal_stdin_stdout_must_both",
	))
}

var checkStartupCopy = install.CheckStartupCopy

// Run is the default TUI entry point used when no subcommand is given.
func Run(_ []string) int {
	postInstall := os.Getenv(install.EnvPostInstall) != ""
	_ = os.Unsetenv(install.EnvPostInstall)

	if err := requireTerminal(); err != nil {
		return fail(err)
	}
	// Missing config always opens the board + interface options; the install
	// wizard is only reached through `kander install`, never bare `kander`.
	configExists, err := config.Exists()
	if err != nil {
		return fail(err)
	}
	if !postInstall && configExists {
		if handled, code := checkStartupCopy(); handled {
			return code
		}
	}
	if !configExists {
		// The first launch probes the environment with doctor and produces a usable config; a failed health check does not stop the user from fixing the options.
		firstLaunchDoctor(postInstall)
	}
	config.BindEffectiveLanguage()
	ctx := tuiPageContext()
	prefs := loadPrefs()
	if prefs.Refresh < minRefreshSecs {
		return fail(fmt.Errorf("%s", t("tui.refresh_interval_must_be_1_second")))
	}
	if !containsString(themes, prefs.Theme) {
		return fail(fmt.Errorf("%s: %s", ctx.UnknownTheme, prefs.Theme))
	}
	root, err := board.BoardRoot()
	if err != nil {
		// A missing board still opens the TUI so first-run and out-of-project
		// launches can show the empty-board welcome instead of exiting.
		if !board.IsBoardNotFound(err) {
			return fail(err)
		}
		root = ""
	}
	app := newApp(prefs.Single, prefs.Refresh, ctx, nil, nil, prefs.Theme, prefs.Columns, saveColumns, copyToClipboard)
	app.IssueProvider = cli.IssueProvider
	attachBoard(app, root)
	initial, err := app.GetBoard()
	if err != nil {
		return fail(err)
	}
	app.MinColumnWidth = clampMinColumnWidth(prefs.MinColumnWidth)
	app.Model.SetBoard(initial)
	app.showJournalWarnings(initial.Warnings)
	if postInstall || !configExists {
		app.openOptionsAt(sectionInterface)
	}
	if err := runBoardTUI(app); err != nil {
		return fail(err)
	}
	return 0
}

// attachBoard binds every board-backed TUI operation to root. An empty root
// keeps the empty-board welcome and offers kander init from chat, import, and
// takeover. A successful init calls this again with the created path.
func attachBoard(app *App, root string) {
	app.boardRoot = strings.TrimSpace(root)
	app.missingBoard = app.boardRoot == ""
	if app.summaries != nil {
		app.summaries.Close()
		app.summaries = nil
	}
	app.GetBoard = func() (BoardPayload, error) {
		if app.boardRoot == "" {
			return BoardPayload{}, nil
		}
		return loadBoardPayload(app.boardRoot)
	}
	app.GetBoardCtx = func(ctx context.Context) (BoardPayload, error) {
		if app.boardRoot == "" {
			return BoardPayload{}, nil
		}
		return board.BoardPayloadContext(ctx, app.boardRoot)
	}
	if app.boardRoot != "" {
		index, err := board.NewSummaryIndex(app.boardRoot, board.StrongInterval(app.RefreshSecs))
		if err == nil {
			app.summaries = index
			app.GetBoard = func() (BoardPayload, error) {
				return index.View(context.Background())
			}
			app.GetBoardCtx = index.View
		}
	}
	app.GetTask = func(id string) (Task, error) {
		if app.boardRoot == "" {
			return Task{}, fmt.Errorf("%s", t("board.board_directory_not_found_run_inside_a_project_or"))
		}
		return loadTaskPayload(app.boardRoot, id)
	}
	app.GetTaskCtx = func(ctx context.Context, id string) (Task, error) {
		if app.boardRoot == "" {
			return Task{}, fmt.Errorf("%s", t("board.board_directory_not_found_run_inside_a_project_or"))
		}
		return board.TaskPayloadContext(ctx, app.boardRoot, id)
	}
	app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
		if strings.TrimSpace(options.Language) == "" {
			options.Language = configuredAgentLanguage()
		}
		return issue.Import(ctx, cli.IssueProvider(), app.boardRoot, repository, number, options)
	}
	app.ImportIndex = func() (issue.Index, error) {
		if app.boardRoot == "" {
			return issue.Index{}, nil
		}
		return issue.LoadIndex(app.boardRoot)
	}
	app.LoadIssueCache = func(repository issue.Repository, number int) (issue.IssueSnapshot, bool) {
		if app.boardRoot == "" {
			return issue.IssueSnapshot{}, false
		}
		return issue.ReadCachedSnapshot(app.boardRoot, repository, number)
	}
	app.SaveIssueCache = func(snapshot issue.IssueSnapshot) error {
		if app.boardRoot == "" {
			return nil
		}
		return issue.WriteCachedSnapshot(app.boardRoot, snapshot, issue.DefaultCacheBounds())
	}
	app.PrepareTriage = func() (launch.TriagePreview, error) {
		return launch.PreviewTriage("", "")
	}
	app.TriageIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		if app.boardRoot == "" {
			return issue.TriageOutcome{}, errors.New(t("board.board_directory_not_found_run_inside_a_project_or"))
		}
		return issue.StartTriage(ctx, cli.IssueProvider(), app.boardRoot, repository, number, options)
	}
	app.ResultIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		provider, ok := cli.IssueProvider().(issue.ResultProvider)
		if !ok {
			return issue.TriageOutcome{}, errors.New(t("tui.issues_takeover_unavailable"))
		}
		return issue.StartResult(ctx, provider, app.boardRoot, repository, number, options)
	}
	if app.boardRoot == "" {
		app.PrepareChat = nil
		app.StartChat = nil
	} else {
		app.PrepareChat = launch.PreviewChat
		app.StartChat = func(message string) (launch.ChatResult, error) {
			return launch.StartChat(launch.ChatRequest{Root: app.boardRoot, Message: message})
		}
	}
	app.PreviewBoardInit = func() (string, error) { return board.PlannedInitRoot("") }
	app.InitBoard = func() (string, error) {
		created, _, _, err := board.InitBoard("")
		return created, err
	}
	app.AttachBoard = func(created string) { attachBoard(app, created) }
}

// firstLaunchDoctor creates the missing config through doctor. doctor names a new
// config's language only from a CLI override, so after an install handoff the
// wizard language, carried as the KANDER_LANG default, acts as that override for
// this creation only; the bound config language decides everything afterwards.
func firstLaunchDoctor(postInstall bool) {
	if postInstall && config.CLILanguage() == "" {
		config.ApplyLanguageArgument([]string{"kander", "--lang", config.ResolveScopeLanguage()})
		defer config.ApplyLanguageArgument(nil)
	}
	_ = menu.Doctor(nil)
}
