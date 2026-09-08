package tui

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

func TestFlowRootAndReadOnlySession(t *testing.T) {
	app, panel := openPanel(t)
	root := ansi.Strip(panel.form.View())
	if a, b, c := strings.Index(root, config.Text("rules.modules")), strings.Index(root, config.Text("flow.title")), strings.Index(root, config.Text("tui.environment_check")); a < 0 || b <= a || c <= b {
		t.Fatalf("wrong root order: %s", root)
	}
	before := config.Clone(panel.session.Config)
	path := os.Getenv(config.EnvConfig)
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	moveRootTo(t, panel, sectionFlow)
	drivePanel(panel, keyMsg("enter"))
	if panel.report == nil || panel.report.title != config.Text("flow.title") || panel.dirty || app.pendingWork != nil || app.pendingShell != nil {
		t.Fatal("flow must open a synchronous, clean, read-only report")
	}
	drivePanel(panel, keyMsg("esc"))
	if panel.report != nil || panel.form == nil || panel.section != sectionFlow || panel.current != "" {
		t.Fatal("Escape must restore the root and selected flow entry")
	}
	if !reflect.DeepEqual(before, panel.session.Config) {
		t.Fatal("viewing changed configuration")
	}
	// Make an unsaved change through the existing section binding, then reopen.
	pumpPanel(panel, panel.dispatch(sectionRules))
	*panel.bind.rules[config.RuleReview] = false
	panel.bind.apply(panel)
	drivePanel(panel, keyMsg("esc"))
	panel.dispatch(sectionFlow)
	if !panel.dirty || !strings.Contains(panel.report.title, config.Text("tui.unsaved")) {
		t.Fatal("unsaved state must be retained and visible")
	}
	for _, line := range panel.report.lines {
		if strings.Contains(line.Text, "PM:") || strings.Contains(line.Text, "QA:") {
			t.Fatal("flow used saved config instead of the session")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(disk) != string(after) {
		t.Fatal("read-only flow wrote disk configuration")
	}
}

func TestFlowNarrowReportScrolling(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		t.Run(lang, func(t *testing.T) {
			config.ApplyLanguageArgument([]string{"--lang", lang})
			defer config.ApplyLanguageArgument(nil)
			app, panel := openPanel(t)
			app.Width, app.Height = 72, 12
			panel.session.Config.Models.Kanban["codex"]["large_model"] = strings.Repeat("model-", 18)
			panel.dispatch(sectionFlow)
			if panel.innerWidth() != 60 {
				t.Fatalf("inner width %d", panel.innerWidth())
			}
			panel.view()
			for _, line := range strings.Split(panel.report.view.View(), "\n") {
				if !utf8.ValidString(line) || ansi.StringWidth(line) > 60 || strings.Contains(line, "�") {
					t.Fatalf("broken narrow output: %q", line)
				}
			}
			for _, msg := range []tea.Msg{tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyPgDown}} {
				old := panel.report.view.YOffset
				panel.Update(msg)
				if panel.report.view.YOffset <= old {
					t.Fatal("keyboard must scroll report")
				}
			}
			old := panel.report.view.YOffset
			panel.Update(tea.KeyMsg{Type: tea.KeyPgUp})
			if panel.report.view.YOffset >= old {
				t.Fatal("PgUp must scroll upward")
			}
			old = panel.report.view.YOffset
			panel.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			if panel.report.view.YOffset <= old {
				t.Fatal("wheel must scroll report")
			}
			// Inspect every wrapped line, including text below the first viewport.
			panel.report.view.Height = panel.report.view.TotalLineCount()
			panel.report.view.GotoTop()
			text := ansi.Strip(panel.report.view.View())
			for _, line := range strings.Split(text, "\n") {
				if !utf8.ValidString(line) || ansi.StringWidth(line) > 60 {
					t.Fatalf("broken wrapped line: %q", line)
				}
			}
			if !strings.Contains(text, i18n.Text(lang, "flow.execution")) || !strings.Contains(text, i18n.Text(lang, "flow.review")) {
				t.Fatal("missing localized execution or review section")
			}
		})
	}
}

func TestFlowShowsUnsavedModelsAndCLIDefault(t *testing.T) {
	_, panel := openPanel(t)
	panel.session.Config.Models.Kanban["codex"]["large_model"] = "unsaved-large"
	panel.session.Config.Models.ReviewRoles["PM"]["model"] = "unsaved-pm"
	panel.session.Config.Models.Kanban["codex"]["small_model"] = ""
	panel.session.Config.Models.Kanban["codex"]["model"] = ""
	panel.dispatch(sectionFlow)
	var text string
	for _, line := range panel.report.lines {
		text += line.Text + "\n"
	}
	for _, want := range []string{"unsaved-large", "unsaved-pm", config.Text("flow.cli_default")} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
