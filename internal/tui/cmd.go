package tui

import (
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

func init() {
	// 不带子命令时直接进看板, 全部界面都发生在 alt-screen 内.
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

// Run 是不带子命令时的默认 TUI 入口.
func Run(_ []string) int {
	if err := requireTerminal(); err != nil {
		return fail(err)
	}
	configExists, err := config.Exists()
	if err != nil {
		return fail(err)
	}
	if !configExists {
		// 首次启动先用 doctor 探测环境并生成适用配置; 健康检查失败不阻止用户修正选项.
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
	if err != nil {
		return fail(err)
	}
	getBoard := func() (BoardPayload, error) { return loadBoardPayload(root) }
	getTask := func(id string) (Task, error) { return loadTaskPayload(root, id) }
	initial, err := getBoard()
	if err != nil {
		return fail(err)
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
