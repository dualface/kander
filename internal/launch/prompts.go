package launch

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

// commandName is the kander command as prompts show it; a project install names its entry through
// process.AgentPath.
func commandName(paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		return process.AgentPath(filepath.Join(paths.BinDir, "kander"))
	}
	return "kander"
}

// rulesPath is the displayed path of one released rules file in the install scope.
func rulesPath(paths config.InstallPaths, name string) string {
	return process.AgentPath(filepath.Join(paths.RulesDir, name))
}

// render executes one prompt template below templates/prompt with data. The installed copy
// wins over the embedded one; a missing installed copy is reported once on standard error.
func render(paths config.InstallPaths, name string, data any) (string, error) {
	return config.RenderRulesTemplate(paths, "templates/prompt/"+name, data, os.Stderr)
}

// mustRender is for prompt fragments whose data cannot fail validation; a template error is a
// broken installation and is returned to the caller as a launch error elsewhere.
func mustRender(paths config.InstallPaths, name string, data any) string {
	text, err := render(paths, name, data)
	if err != nil {
		return ""
	}
	return text
}

// RuleLoadingInstruction is shared by fresh launches and notifications to existing Agents.
// It names the bootstrap before the command contract so optional rules cannot be loaded first.
func RuleLoadingInstruction(paths config.InstallPaths) string {
	return mustRender(paths, "rule-loading.md", map[string]string{
		"AgentsRules": rulesPath(paths, "KANDER-AGENTS.md"),
		"Command":     commandName(paths),
		"KanbanRules": rulesPath(paths, "KANDER-KANBAN-RULES.md"),
	})
}

func promptAgents(paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		return process.AgentPath(filepath.Join(paths.ProjectRoot, "AGENTS.md"))
	}
	return mustRender(paths, "agents-default.md", nil)
}

// SelfMoveInstruction produces the "move yourself back to working before handling this" requirement while the card sits in review;
// other states return an empty string. notify/resume no longer move cards, the notified execution agent does it.
func SelfMoveInstruction(paths config.InstallPaths, taskID, state string) string {
	if state != "review" {
		return ""
	}
	return mustRender(paths, "self-move-review.md", map[string]string{"Command": commandName(paths), "TaskID": taskID})
}

func cardStateStatus(paths config.InstallPaths, taskID, state string) string {
	if instruction := SelfMoveInstruction(paths, taskID, state); instruction != "" {
		return instruction
	}
	return mustRender(paths, "card-working.md", nil)
}

// resolvePromptLanguage returns the agent communication language for start/resume/takeover
// prompts and notify direct delivery. Card LANGUAGE wins; when the field is absent it falls
// back to agent_language from the process config scope (ConfigPath / KANDER_CONFIG), matching
// kander new (a missing or invalid config is an error, not a derived default).
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

// promptLanguageDirective renders the fixed language instruction embedded in launch prompts.
func promptLanguageDirective(paths config.InstallPaths, language string) string {
	return mustRender(paths, "language.md", map[string]string{"Language": language})
}

// RuleLoadingWithLanguage appends the card language directive after the rule-loading instruction.
// Used by start/resume/takeover task files and by notify direct delivery.
func RuleLoadingWithLanguage(paths config.InstallPaths, cardText string) (string, error) {
	return ruleLoadingWithLanguage(paths, cardText)
}

// ruleLoadingWithLanguage joins the preamble fragments: rule loading, language directive, the
// reporting reminder while rules.reporting is enabled, and the SIZE sentence.
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
	parts := []string{RuleLoadingInstruction(paths), promptLanguageDirective(paths, lang)}
	if reporting := promptReportingDirective(paths); reporting != "" {
		parts = append(parts, reporting)
	}
	parts = append(parts, mustRender(paths, "size.md", map[string]string{"Size": size}))
	return strings.Join(parts, " "), nil
}

// promptReportingDirective is the completion-report reminder embedded in launch prompts while
// rules.reporting is enabled; it is empty otherwise so the prompt stays unchanged. An
// unreadable config also leaves it out: a card with LANGUAGE must still launch without a
// config, and the reporting rule itself is read through the config the agent loads first.
func promptReportingDirective(paths config.InstallPaths) string {
	cfg, err := config.Load(false)
	if err != nil || !cfg.Rules[config.RuleReporting] {
		return ""
	}
	return mustRender(paths, "reporting.md", nil)
}

// promptHead renders the one-line head that opens a task file and the argv instruction.
func promptHead(paths config.InstallPaths, kind, taskID, target string, count int) string {
	return mustRender(paths, "head.md", map[string]any{"Kind": kind, "TaskID": taskID, "Target": target, "Count": count})
}

// taskFileTrailer renders the delete-when-done note appended to every task file.
func taskFileTrailer(paths config.InstallPaths) process.TaskFileTrailer {
	return process.TaskFileTrailerFor(paths.RulesDir)
}

// taskFileInstruction builds the one-line file instruction handed to the agent CLI.
func taskFileInstruction(paths config.InstallPaths, head, path string) string {
	return process.TaskFileInstruction(paths.RulesDir, head, path)
}

func startAgentPrompt(taskID string, paths config.InstallPaths, taskGroup, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	preamble, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	return render(paths, "start.md", map[string]any{
		"Head":          promptHead(paths, "start", taskID, "", 0),
		"Preamble":      preamble,
		"Command":       commandName(paths),
		"TaskID":        taskID,
		"Agents":        promptAgents(paths),
		"Group":         taskGroup != "",
		"PhaseSnippets": "",
	})
}

func resumeAgentPrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	return resumePrompt(taskID, message, paths, state, cardText)
}

func promptStatus(paths config.InstallPaths, taskID, state, cardText string) string {
	if board.MetadataFrom(cardText, "DISPATCH_ID") != "" {
		return mustRender(paths, "dispatch-bound.md", nil)
	}
	return cardStateStatus(paths, taskID, state)
}

func resumePrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	preamble, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	return render(paths, "resume.md", map[string]any{
		"Head":          promptHead(paths, "resume", taskID, "", 0),
		"Status":        promptStatus(paths, taskID, state, cardText),
		"Preamble":      preamble,
		"Command":       commandName(paths),
		"TaskID":        taskID,
		"Message":       message,
		"Agents":        promptAgents(paths),
		"PhaseSnippets": "",
	})
}

func takeoverAgentPrompt(taskID, message string, paths config.InstallPaths, previous, state, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	preamble, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	return render(paths, "takeover.md", map[string]any{
		"Head":          promptHead(paths, "takeover", taskID, "", 0),
		"Previous":      previous,
		"Status":        promptStatus(paths, taskID, state, cardText),
		"Preamble":      preamble,
		"Command":       commandName(paths),
		"TaskID":        taskID,
		"Message":       message,
		"Agents":        promptAgents(paths),
		"PhaseSnippets": "",
	})
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
