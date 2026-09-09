package launch

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/board"
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

// resolvePromptLanguage returns the agent communication language for start/resume/takeover
// prompts and notify direct delivery. Card LANGUAGE wins; when the field is absent it falls
// back to agent_language from the process config scope (ConfigPath / KANDER_CONFIG), matching
// kander new (a missing or invalid config is an error, not a derived default).
// paths is the install scope used alongside this language for rule-loading paths in the same
// prompt; config I/O intentionally follows ConfigPath rather than paths.ConfigPath so
// KANDER_CONFIG overrides remain consistent with the rest of the tool.
func resolvePromptLanguage(cardText string, paths config.InstallPaths) (string, error) {
	if lang := strings.TrimSpace(metadataFrom(cardText, board.FieldLanguage)); lang != "" {
		return lang, nil
	}
	_ = paths
	cfg, err := config.Load(false)
	if err != nil {
		return "", err
	}
	return cfg.AgentLanguage, nil
}

// promptLanguageDirective is the fixed English hard instruction embedded in launch prompts.
func promptLanguageDirective(language string) string {
	return `Communicate with the user and write all card content, records and reports in "` + language +
		`". Commit messages and code comments follow the project's conventions.`
}

// RuleLoadingWithLanguage appends the card language directive after the rule-loading instruction.
// Used by start/resume/takeover task files and by notify direct delivery.
func RuleLoadingWithLanguage(paths config.InstallPaths, cardText string) (string, error) {
	return ruleLoadingWithLanguage(paths, cardText)
}

// ruleLoadingWithLanguage appends the language directive after the rule-loading instruction.
// A trailing space keeps the localized template suffix from gluing onto the English sentence.
func ruleLoadingWithLanguage(paths config.InstallPaths, cardText string) (string, error) {
	lang, err := resolvePromptLanguage(cardText, paths)
	if err != nil {
		return "", err
	}
	size := board.MetadataFrom(cardText, board.FieldSize)
	if size == "" {
		size = "large"
	}
	if size != "small" && size != "large" {
		return "", launchError("board.size_invalid", "prompt")
	}
	return RuleLoadingInstruction(paths) + promptLanguageDirective(lang) + " " + t("launch.prompt.size", size) + " ", nil
}

func startAgentPrompt(taskID string, paths config.InstallPaths, taskGroup, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	rules, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	cmd := commandName(paths)
	ending := t("launch.prompt.start_single")
	if taskGroup != "" {
		ending = t("launch.prompt.start_group", cmd, taskID)
	}
	return t("launch.prompt.start", t("launch.prompt.start_head", taskID), rules, cmd, taskID, promptAgents(paths), ending), nil
}

func resumeAgentPrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	return resumePrompt(taskID, message, paths, state, cardText)
}

func resumePrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	rules, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	status := cardStateStatus(paths, taskID, state)
	if board.MetadataFrom(cardText, "DISPATCH_ID") != "" {
		status = t("launch.dispatch_bound")
	}
	return t("launch.prompt.resume", t("launch.prompt.resume_head", taskID), status, rules, commandName(paths), taskID, message, promptAgents(paths)), nil
}

func takeoverAgentPrompt(taskID, message string, paths config.InstallPaths, previous, state, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	rules, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	status := cardStateStatus(paths, taskID, state)
	if board.MetadataFrom(cardText, "DISPATCH_ID") != "" {
		status = t("launch.dispatch_bound")
	}
	return t("launch.prompt.takeover", t("launch.prompt.takeover_head", taskID), previous, status, rules, commandName(paths), taskID, message, promptAgents(paths)), nil
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
