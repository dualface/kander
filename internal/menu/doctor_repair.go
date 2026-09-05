package menu

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

func repairDoctorConfig(agents map[string]agentState, tools TerminalTools) (*config.Config, bool) {
	var changes []string
	language := "en"
	// 配置结果提示只使用磁盘配置的语言, 不受界面或进程语言覆盖影响.
	text := func(id string, args ...any) string {
		return i18n.Text(language, id, args...)
	}

	cfg, result, err := config.Repair(func(cfg *config.Config) {
		language = cfg.Language
		changes = repairConfiguredTools(cfg, agents, tools)
	})
	if result.BackupPath != "" {
		hint(text("menu.original_config_backed_up") + result.BackupPath)
	}
	if err != nil {
		warning(text("menu.config_repair_failed") + err.Error())
		return nil, false
	}
	if result.Created {
		success(text("menu.created_and_saved_config_json") + result.Path)
	} else if result.Changed {
		success(text("menu.updated_and_saved_config_json") + result.Path)
	} else {
		success(text("menu.config_json_is_unchanged") + result.Path)
	}
	for _, change := range changes {
		hint(change)
	}
	return cfg, true
}

func repairConfiguredTools(cfg *config.Config, agents map[string]agentState, tools TerminalTools) []string {
	var changes []string
	choose := func(names []string, review bool) string {
		for _, name := range names {
			if agentUsable(agents[name]) && (!review || agents[name].Review) {
				return name
			}
		}
		return ""
	}
	set := func(field string, old *string, replacement string) {
		if replacement == "" || *old == replacement {
			return
		}
		changes = append(changes, field+": "+*old+" -> "+replacement)
		*old = replacement
	}
	execution := choose(config.ExecutionAgents, false)
	if !agentUsable(agents[cfg.KanbanAgent]) {
		set("kanban_agent", &cfg.KanbanAgent, execution)
	}
	for _, scale := range config.TaskScales {
		selected := cfg.KanbanAgents[scale]
		if !agentUsable(agents[selected]) && execution != "" {
			set("kanban_agents."+scale, &selected, cfg.KanbanAgent)
			cfg.KanbanAgents[scale] = selected
		}
	}
	reviewer := choose(config.ReviewAgents, true)
	for _, role := range config.ReviewRoles {
		selected := cfg.Reviewers[role]
		if (!agentUsable(agents[selected]) || !agents[selected].Review) && reviewer != "" {
			set("reviewers."+role, &selected, reviewer)
			cfg.Reviewers[role] = selected
			// 模型与 Reviewer 绑定; 改选时使用新 Reviewer 的模型设置.
			entry := cfg.Models.Review[selected]
			cfg.Models.ReviewRoles[role] = map[string]string{"model": entry["model"], "effort": entry["effort"]}
		}
	}
	if !doctorLauncherAvailable(cfg.Launcher, tools) {
		replacement := "foreground"
		switch {
		case isWindowsOS():
			replacement = "console"
		case tools.Herdr.Available():
			replacement = "herdr"
		case tools.Tmux.Available():
			replacement = "tmux-session"
		}
		set("launcher", &cfg.Launcher, replacement)
	}
	// 有可执行 Agent 和 Reviewer 时, 修复后的选择应立即成为有效配置.
	if execution != "" && reviewer != "" {
		cfg.WelcomeComplete = true
	}
	return changes
}

func doctorLauncherAvailable(launcher string, tools TerminalTools) bool {
	if launcher == "foreground" {
		return true
	}
	if isWindowsOS() {
		return launcher == "console"
	}
	switch launcher {
	case "auto":
		return tools.Herdr.Available() || tools.Tmux.Available()
	case "herdr":
		return tools.Herdr.Available()
	case "tmux", "tmux-session":
		return tools.Tmux.Available()
	}
	return false
}
