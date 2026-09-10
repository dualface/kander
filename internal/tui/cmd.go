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

// Run is the default TUI entry point used when no subcommand is given.
func Run(_ []string) int {
	postInstall := os.Getenv(install.EnvPostInstall) != ""
	_ = os.Unsetenv(install.EnvPostInstall)

	if err := requireTerminal(); err != nil {
		return fail(err)
	}
	if !postInstall {
		runWizard, err := install.ShouldRunWizard()
		if err != nil {
			return fail(err)
		}
		if runWizard {
			return install.RunInteractive()
		}
	}
	configExists, err := config.Exists()
	if err != nil {
		return fail(err)
	}
	if !configExists {
		// The first launch probes the environment with doctor and produces a usable config; a failed health check does not stop the user from fixing the options.
		_ = menu.Doctor(nil)
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
	emptyBoard := false
	if err != nil {
		tolerateMissing := postInstall || !configExists
		if !tolerateMissing || !board.IsBoardNotFound(err) {
			return fail(err)
		}
		emptyBoard = true
	}
	getBoard := func() (BoardPayload, error) {
		if emptyBoard {
			return BoardPayload{}, nil
		}
		return loadBoardPayload(root)
	}
	getTask := func(id string) (Task, error) {
		if emptyBoard {
			return Task{}, fmt.Errorf("%s", t("board.board_directory_not_found_run_inside_a_project_or"))
		}
		return loadTaskPayload(root, id)
	}
	initial := BoardPayload{}
	if !emptyBoard {
		initial, err = getBoard()
		if err != nil {
			return fail(err)
		}
	}
	app := newApp(prefs.Single, prefs.Refresh, ctx, getBoard, getTask, prefs.Theme, prefs.Columns, saveColumns, copyToClipboard)
	app.IssueProvider = cli.IssueProvider
	app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
		if strings.TrimSpace(options.Language) == "" {
			options.Language = configuredAgentLanguage()
		}
		return issue.Import(ctx, cli.IssueProvider(), root, repository, number, options)
	}
	app.ImportIndex = func() (issue.Index, error) {
		if emptyBoard {
			return issue.Index{}, nil
		}
		return issue.LoadIndex(root)
	}
	app.PrepareHandoff = func(_ issue.Repository, _ int, taskID string) (handoffCard, error) {
		return prepareHandoffCard(root, taskID)
	}
	app.SaveHandoff = func(request handoffSaveRequest) (uint64, error) {
		if err := board.UpdateDocument(root, request.taskID, board.UpdateOptions{
			Document: "spec.md", Text: request.text, ExpectedRevision: request.revision,
		}); err != nil {
			return 0, err
		}
		snapshot, err := board.ReadSnapshot(root, request.taskID)
		if err != nil {
			// The write succeeded; leaving the revision unknown makes the next
			// save reconcile through the ordinary conflict path.
			return 0, nil
		}
		return snapshot.Revision, nil
	}
	app.StartHandoff = func(request startRequest) (launch.StartResult, string, error) {
		return runHandoffStart(root, request)
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
