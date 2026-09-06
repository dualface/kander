package launch

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

func commandName(paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		return filepath.Join(paths.BinDir, "kander")
	}
	return "kander"
}

// RuleLoadingInstruction is shared by fresh launches and notifications to existing Agents.
// It names the bootstrap before the command contract so optional rules cannot be loaded first.
func RuleLoadingInstruction(paths config.InstallPaths) string {
	entry := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	contract := filepath.Join(paths.RulesDir, "KANDER-KANBAN-RULES.md")
	return t("launch.prompt.rule_loading", entry, commandName(paths), contract)
}

func promptAgents(paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		return filepath.Join(paths.ProjectRoot, "AGENTS.md")
	}
	return t("launch.the_target_project_s_agents_md")
}

// SelfMoveInstruction produces the "move yourself back to working before handling this" requirement while the card sits in review;
// other states return an empty string. notify/resume no longer move cards, the notified execution agent does it.
func SelfMoveInstruction(paths config.InstallPaths, taskID, state string) string {
	if state != "review" {
		return ""
	}
	return t("launch.prompt.self_move_review", commandName(paths), taskID)
}

func cardStateStatus(paths config.InstallPaths, taskID, state string) string {
	if instruction := SelfMoveInstruction(paths, taskID, state); instruction != "" {
		return instruction
	}
	return t("launch.prompt.working")
}

func startAgentPrompt(taskID string, paths config.InstallPaths, taskGroup string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	cmd := commandName(paths)
	ending := t("launch.prompt.start_single")
	if taskGroup != "" {
		ending = t("launch.prompt.start_group", cmd, taskID)
	}
	return t("launch.prompt.start", t("launch.prompt.start_head", taskID), RuleLoadingInstruction(paths), cmd, taskID, promptAgents(paths), ending), nil
}

func resumeAgentPrompt(taskID, message string, paths config.InstallPaths, state string) (string, error) {
	return resumePrompt(taskID, message, paths, state)
}

func resumePrompt(taskID, message string, paths config.InstallPaths, state string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	status := cardStateStatus(paths, taskID, state)
	return t("launch.prompt.resume", t("launch.prompt.resume_head", taskID), status, RuleLoadingInstruction(paths), commandName(paths), taskID, message, promptAgents(paths)), nil
}

func takeoverAgentPrompt(taskID, message string, paths config.InstallPaths, previous, state string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	status := cardStateStatus(paths, taskID, state)
	return t("launch.prompt.takeover", t("launch.prompt.takeover_head", taskID), previous, status, RuleLoadingInstruction(paths), commandName(paths), taskID, message, promptAgents(paths)), nil
}

func readTaskMessage(message string, messageSet bool, messageFile string, command string) (string, error) {
	if messageSet == (messageFile != "") {
		return "", launchError(
			"launch.requires_exactly_one_of_message_or_message_file", command,
		)
	}
	text := message
	if messageFile != "" {
		if fs.IsReparsePoint(messageFile) {
			return "", launchError(
				"launch.message_file_must_not_be_a_symlink_reparse_point", messageFile,
			)
		}
		data, err := os.ReadFile(messageFile)
		if err != nil {
			return "", launchError("launch.failed_to_read_message_file", err.Error())
		}
		text = string(data)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", launchError("launch.message_must_not_be_empty", command)
	}
	return text, nil
}
