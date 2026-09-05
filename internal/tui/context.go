package tui

import "github.com/dualface/kander/internal/config"

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

type pageContext struct {
	Title            string
	Search           string
	Updated          string
	Empty            string
	Unassigned       string
	Error            string
	TooSmall         string
	QuitHelp         string
	SearchHelp       string
	Columns          string
	ColumnUnit       string
	CardUnit         string
	StatusHelp       string
	DetailStatusHelp string
	ThemeLabels      map[string]string
	Help             string
	DetailHelp       string
	Copied           string
	CopyFailed       string
	ClipboardNA      string
	NoMatch          string
	TermInitFail     string
	UnknownTheme     string
	StateLabels      map[string]string
	SizeLabels       map[string]string
}

func tuiPageContext() pageContext {
	return pageContext{
		Title:            t("tui.task_board"),
		Search:           t("tui.search"),
		Updated:          t("tui.updated"),
		Empty:            t("tui.no_tasks"),
		Unassigned:       t("tui.unassigned"),
		Error:            t("tui.load_failed"),
		TooSmall:         t("tui.terminal_is_too_small_enlarge_the_window"),
		QuitHelp:         t("tui.q_quit_o_options"),
		SearchHelp:       t("tui.enter_apply_esc_clear"),
		Columns:          t("tui.columns"),
		ColumnUnit:       t("tui.cols"),
		CardUnit:         t("tui.cards"),
		StatusHelp:       t("tui.help_q_quit"),
		DetailStatusHelp: t("tui.help_q_back"),
		ThemeLabels: map[string]string{
			"auto":  t("tui.auto"),
			"light": t("tui.light"),
			"dark":  t("tui.dark"),
		},
		Help: t(
			"tui.arrows_hjkl_mouse_move_double_click_detail_drag_to",
		),
		DetailHelp: t(
			"tui.hjkl_arrows_move_cursor_wheel_scroll_ctrl_d_u",
		),
		Copied:       t("tui.copied"),
		CopyFailed:   t("tui.copy_failed"),
		ClipboardNA:  t("tui.clipboard_unavailable"),
		NoMatch:      t("tui.no_match"),
		TermInitFail: t("tui.failed_to_initialize_terminal"),
		UnknownTheme: t("tui.unknown_theme"),
		StateLabels: map[string]string{
			"backlog":  t("tui.backlog"),
			"todo":     t("tui.todo"),
			"working":  t("tui.working"),
			"review":   t("tui.review"),
			"done":     t("tui.done"),
			"archived": t("tui.archived"),
			"trash":    t("tui.trash"),
		},
		SizeLabels: map[string]string{
			"small": t("tui.small"),
			"large": t("tui.large"),
		},
	}
}

func (c pageContext) stateLabel(state string) string {
	if label, ok := c.StateLabels[state]; ok {
		return label
	}
	return state
}

func (c pageContext) sizeLabel(kind string) string {
	if label, ok := c.SizeLabels[kind]; ok {
		return label
	}
	if kind == "" {
		return "-"
	}
	return kind
}

func (c pageContext) themeLabel(name string) string {
	if label, ok := c.ThemeLabels[name]; ok {
		return label
	}
	return name
}
