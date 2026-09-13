package menu

import (
	"os"
	"os/exec"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/direct"
	"github.com/dualface/kander/internal/terminal/herdr"
	"github.com/dualface/kander/internal/terminal/tmux"
)

func choicesWithCurrent(choices []Choice, current string) []Choice {
	for _, item := range choices {
		if item.Value == current {
			return choices
		}
	}
	unavailable := config.Text("menu.currently_unavailable")
	labels := agentLabels()
	return append([]Choice{{Value: current, Label: labels[current] + " (" + unavailable + ")"}}, choices...)
}

func reviewStageChoices() []Choice {
	labels := map[string]string{
		"auto":     "menu.labels.auto",
		"skip":     "menu.labels.skip",
		"required": "menu.labels.required",
	}
	out := make([]Choice, 0, len(config.ReviewStageModes))
	for _, mode := range config.ReviewStageModes {
		out = append(out, Choice{Value: mode, Label: config.Text(labels[mode])})
	}
	return out
}

func languageChoices() []Choice {
	out := make([]Choice, 0, len(config.Languages))
	for _, code := range config.Languages {
		out = append(out, Choice{Value: code, Label: config.FormatLanguageSummary(code)})
	}
	return out
}

// agentLanguageFixedChoices is the options-panel list for agent_language.
// Labels are endonyms and are not translated with the interface language.
var agentLanguageFixedChoices = []Choice{
	{Value: "en", Label: "English"},
	{Value: "zh-CN", Label: "简体中文"},
	{Value: "zh-TW", Label: "繁體中文"},
	{Value: "ja", Label: "日本語"},
	{Value: "ko", Label: "한국어"},
	{Value: "es", Label: "Español"},
	{Value: "fr", Label: "Français"},
	{Value: "de", Label: "Deutsch"},
}

// agentLanguageChoices returns the fixed list, appending the current stored value
// when it is outside the list so hand-edited codes are not silently replaced.
func agentLanguageChoices(current string) []Choice {
	out := make([]Choice, len(agentLanguageFixedChoices))
	copy(out, agentLanguageFixedChoices)
	current = strings.TrimSpace(current)
	if current == "" {
		return out
	}
	for _, item := range out {
		if item.Value == current {
			return out
		}
	}
	return append(out, Choice{Value: current, Label: current})
}

func installTmux() bool {
	managers := []struct {
		name string
		argv []string
	}{
		{"brew", []string{"brew", "install", tmux.Executable}},
		{"apt-get", []string{"apt-get", "install", "-y", tmux.Executable}},
		{"dnf", []string{"dnf", "install", "-y", tmux.Executable}},
		{"pacman", []string{"pacman", "-S", "--needed", "--noconfirm", tmux.Executable}},
		{"apk", []string{"apk", "add", tmux.Executable}},
	}
	var selected []string
	for _, manager := range managers {
		if lookPath(manager.name) != "" {
			selected = manager.argv
			break
		}
	}
	if selected == nil {
		warning(config.Text("menu.no_supported_package_manager_was_found_install_tmux_manually"))
		return false
	}
	if selected[0] != "brew" && needsAdmin() {
		sudo := lookPath("sudo")
		if sudo == "" {
			warning(config.Text("menu.installing_tmux_requires_administrator_privileges", strings.Join(selected, " ")))
			return false
		}
		selected = append([]string{sudo}, selected...)
	}
	hint(config.Text("menu.about_to_run_2", strings.Join(selected, " ")))
	cmd := exec.Command(selected[0], selected[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil || lookPath(tmux.Executable) == "" {
		warning(config.Text("menu.tmux_installation_failed_or_tmux_is_still_not_in"))
		return false
	}
	success(config.Text("menu.tmux_is_installed"))
	return true
}

func autoLauncherChoice() Choice {
	return Choice{
		Value: "auto",
		Label: config.Text("menu.choose_herdr_or_tmux_from_the_current_environment"),
	}
}

func tmuxLauncherChoices() []Choice {
	return []Choice{
		{Value: tmux.Name, Label: config.Text("menu.new_window_in_the_current_tmux_session")},
		{Value: tmux.SessionName, Label: config.Text("menu.new_window_in_a_per_project_tmux_session")},
	}
}

func herdrLauncherChoices(cfg *config.Config) []Choice {
	installed := lookPath(herdr.Executable) != ""
	if !installed && cfg.Launcher != herdr.Name {
		return nil
	}
	label := config.Text("menu.new_tab_in_the_current_herdr_workspace")
	if !installed {
		label += config.Text("menu.not_currently_installed")
	}
	return []Choice{{Value: herdr.Name, Label: label}}
}

// windowsLauncherChoices leaves out tmux: native Windows has none.
// herdr has a native Windows build, so it is offered once installed.
func windowsLauncherChoices(cfg *config.Config) []Choice {
	choices := []Choice{{Value: direct.Console, Label: config.Text("menu.separate_windows_console")}}
	choices = append(choices, herdrLauncherChoices(cfg)...)
	return append(choices, Choice{
		Value: direct.Foreground, Label: config.Text("menu.foreground_in_this_terminal"),
	})
}

func copyModels(src config.Models) config.Models {
	out := config.DefaultModels()
	for agent, entry := range src.Kanban {
		if out.Kanban[agent] == nil {
			out.Kanban[agent] = map[string]string{}
		}
		for key, value := range entry {
			out.Kanban[agent][key] = value
		}
	}
	for agent, entry := range src.Review {
		if out.Review[agent] == nil {
			out.Review[agent] = map[string]string{}
		}
		for key, value := range entry {
			out.Review[agent][key] = value
		}
	}
	for role, entry := range src.ReviewRoles {
		if out.ReviewRoles[role] == nil {
			out.ReviewRoles[role] = map[string]string{}
		}
		for key, value := range entry {
			out.ReviewRoles[role][key] = value
		}
	}
	return out
}
