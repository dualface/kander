package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func requireActionValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s", t("actions.required"))
	}
	return nil
}

func (a *App) openTaskActionForm(action taskAction) tea.Cmd {
	dialog := a.TaskActions
	dialog.action = action
	var groups []*huh.Group
	if action == actionTrash {
		dialog.options.Result = "trashed"
	} else if dialog.source.snapshot.Entry.State == "done" {
		dialog.options.Result = "completed"
	} else {
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title(t("actions.result")).Options(
			huh.NewOption(t("actions.cancelled"), "cancelled"),
			huh.NewOption(t("actions.duplicate"), "duplicate"),
			huh.NewOption(t("actions.wontfix"), "wontfix"),
		).Value(&dialog.options.Result)))
		groups = append(groups, huh.NewGroup(huh.NewInput().Title(t("actions.duplicate_of")).
			Value(&dialog.options.DuplicateOf).Validate(requireActionValue)).
			WithHideFunc(func() bool { return dialog.options.Result != "duplicate" }))
	}
	groups = append(groups,
		huh.NewGroup(huh.NewInput().Title(t("actions.reason")).Value(&dialog.options.Reason).Validate(requireActionValue)),
		huh.NewGroup(huh.NewInput().Title(t("actions.decision")).Value(&dialog.options.Decision).Validate(requireActionValue)),
	)
	dialog.form = huh.NewForm(groups...).WithTheme(huhTheme(themePalette(a.Theme))).WithShowHelp(false).WithShowErrors(true)
	a.resizeTaskActionForm()
	return dialog.form.Init()
}

func (a *App) resizeTaskActionForm() {
	dialog := a.TaskActions
	if dialog == nil || dialog.form == nil {
		return
	}
	h, w := a.size()
	frame := popup{Title: printableText(dialog.id), Hint: t("actions.form_hint"), TightFit: true}
	dialog.form.WithWidth(frame.inner(w, h, 64)).WithHeight(max(1, min(8, h-frame.chrome())))
}

func (a *App) updateTaskActions(msg tea.Msg) tea.Cmd {
	dialog := a.TaskActions
	if dialog.running {
		return nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && mapKey(key) == "esc" {
		a.TaskActions = nil
		return nil
	}
	if dialog.form == nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			return a.handleTaskActionKey(mapKey(key))
		}
		return nil
	}
	_, cmd := dialog.form.Update(msg)
	a.resizeTaskActionForm()
	if dialog.form.State == huh.StateCompleted {
		if dialog.options.Result != "duplicate" {
			dialog.options.DuplicateOf = ""
		}
		a.queueTaskAction()
	} else if dialog.form.State == huh.StateAborted {
		a.TaskActions = nil
	}
	return cmd
}
