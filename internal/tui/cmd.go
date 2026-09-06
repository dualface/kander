package tui

import (
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
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

func requireTerminal() error {
	if isTTY(os.Stdin) && isTTY(os.Stdout) {
		return nil
	}
	return errors.New(t(
		"tui.tui_requires_an_interactive_terminal_stdin_stdout_must_both",
	))
}

// Run is the default TUI entry point used when no subcommand is given.
func Run(_ []string) int {
	if err := requireTerminal(); err != nil {
		return fail(err)
	}
	runWizard, err := install.ShouldRunWizard()
	if err != nil {
		return fail(err)
	}
	if runWizard {
		return install.RunInteractive()
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
		if configExists || !board.IsBoardNotFound(err) {
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
	app.MinColumnWidth = clampMinColumnWidth(prefs.MinColumnWidth)
	app.Model.SetBoard(initial)
	if !configExists {
		app.openOptionsAt(sectionInterface)
	}
	if err := runTUI(app); err != nil {
		return fail(err)
	}
	return 0
}
