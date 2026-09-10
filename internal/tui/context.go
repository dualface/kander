package tui

import "github.com/dualface/kander/internal/config"

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

type pageContext struct {
	Title               string
	Search              string
	Updated             string
	Empty               string
	Unassigned          string
	Error               string
	TooSmall            string
	QuitHelp            string
	SearchHelp          string
	Columns             string
	ColumnUnit          string
	CardUnit            string
	StatusHelp          string
	DetailStatusHelp    string
	ThemeLabels         map[string]string
	Help                string
	DetailHelp          string
	Copied              string
	CopyFailed          string
	ClipboardNA         string
	NoMatch             string
	TermInitFail        string
	UnknownTheme        string
	IssuesTitle         string
	IssuesHint          string
	IssuesLoading       string
	IssuesEmpty         string
	IssuesMore          string
	IssuesLoadFailed    string
	IssuesSearchPrompt  string
	IssuesLabelPrompt   string
	IssuesDetailHint    string
	IssuesNoTarget      string
	IssuesBrowserOpened string
	IssuesBrowserFailed string
	IssuesNoBody        string
	IssuesImporting     string
	IssuesImportFailed  string
	IssuesImportNoCard  string
	StateLabels         map[string]string
	IssueStateLabels    map[string]string
	SizeLabels          map[string]string
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
			"tide":           t("tui.tide"),
			"dusk":           t("tui.dusk"),
			"slate-dark":     t("tui.slate-dark"),
			"slate-light":    t("tui.slate-light"),
			"auto":           t("tui.auto"),
			"light":          t("tui.light"),
			"light-warm":     t("tui.light-warm"),
			"light-contrast": t("tui.light-contrast"),
			"dark":           t("tui.dark"),
			"dark-soft":      t("tui.dark-soft"),
			"dark-contrast":  t("tui.dark-contrast"),
		},
		Help: t(
			"tui.arrows_hjkl_mouse_move_double_click_detail_drag_to",
		),
		DetailHelp: t(
			"tui.hjkl_arrows_move_cursor_wheel_scroll_ctrl_d_u",
		),
		Copied:              t("tui.copied"),
		CopyFailed:          t("tui.copy_failed"),
		ClipboardNA:         t("tui.clipboard_unavailable"),
		NoMatch:             t("tui.no_match"),
		TermInitFail:        t("tui.failed_to_initialize_terminal"),
		UnknownTheme:        t("tui.unknown_theme"),
		IssuesTitle:         t("tui.issues_title"),
		IssuesHint:          t("tui.issues_hint"),
		IssuesLoading:       t("tui.issues_loading"),
		IssuesEmpty:         t("tui.issues_empty"),
		IssuesMore:          t("tui.issues_more"),
		IssuesLoadFailed:    t("tui.issues_load_failed"),
		IssuesSearchPrompt:  t("tui.issues_search_prompt"),
		IssuesLabelPrompt:   t("tui.issues_label_prompt"),
		IssuesDetailHint:    t("tui.issues_detail_hint"),
		IssuesNoTarget:      t("tui.issues_no_target"),
		IssuesBrowserOpened: t("tui.issues_browser_opened"),
		IssuesBrowserFailed: t("tui.issues_browser_failed"),
		IssuesNoBody:        t("tui.issues_no_body"),
		IssuesImporting:     t("tui.issues_importing"),
		IssuesImportFailed:  t("tui.issues_import_failed"),
		IssuesImportNoCard:  t("tui.issues_import_no_card"),
		IssueStateLabels: map[string]string{
			"open":   t("tui.issues_state_open"),
			"closed": t("tui.issues_state_closed"),
			"all":    t("tui.issues_state_all"),
		},
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

func (c pageContext) issueStateLabel(state string) string {
	if label, ok := c.IssueStateLabels[state]; ok {
		return label
	}
	return state
}

func (c pageContext) themeLabel(name string) string {
	if label, ok := c.ThemeLabels[name]; ok {
		return label
	}
	return name
}
