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
// wins over the embedded one; a missing installed copy is reported once on standard error. A
// template that fails to parse or lacks a data key is an error the caller returns, never an
// empty prompt.
func render(paths config.InstallPaths, name string, data any) (string, error) {
	return config.RenderRulesTemplate(paths, "templates/prompt/"+name, data, os.Stderr)
}

// fragments collects the rendered pieces of one prompt and keeps the first render error, so
// a builder reads linearly and still fails as a whole.
type fragments struct {
	paths config.InstallPaths
	err   error
}

func (f *fragments) render(name string, data any) string {
	if f.err != nil {
		return ""
	}
	text, err := render(f.paths, name, data)
	if err != nil {
		f.err = err
		return ""
	}
	return text
}

func promptAgents(paths config.InstallPaths) (string, error) {
	if paths.Mode == config.ModeProject {
		return process.AgentPath(filepath.Join(paths.ProjectRoot, "AGENTS.md")), nil
	}
	return render(paths, "agents-default.md", nil)
}

// SelfMoveInstruction produces the "move yourself back to working before handling this" requirement while the card sits in review;
// other states return an empty string. notify/resume no longer move cards, the notified execution agent does it.
func SelfMoveInstruction(paths config.InstallPaths, taskID, state string) (string, error) {
	if state != "review" {
		return "", nil
	}
	return render(paths, "self-move-review.md", map[string]string{"Command": commandName(paths), "TaskID": taskID})
}

func cardStateStatus(paths config.InstallPaths, taskID, state string) (string, error) {
	instruction, err := SelfMoveInstruction(paths, taskID, state)
	if err != nil || instruction != "" {
		return instruction, err
	}
	return render(paths, "card-working.md", nil)
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
func promptLanguageDirective(paths config.InstallPaths, language string) (string, error) {
	return render(paths, "language.md", map[string]string{"Language": language})
}

// rulesAndLanguage joins the rule-loading instruction and the language directive: the preamble
// of every prompt Kander hands to an agent, for fresh launches and notifications alike. The
// rule-loading sentence names the bootstrap before the command contract so optional rules
// cannot be loaded first.
func rulesAndLanguage(paths config.InstallPaths, language string) (string, error) {
	f := &fragments{paths: paths}
	rules := f.render("rule-loading.md", map[string]string{
		"AgentsRules": rulesPath(paths, "KANDER-AGENTS.md"),
		"Command":     commandName(paths),
		"KanbanRules": rulesPath(paths, "KANDER-KANBAN-RULES.md"),
	})
	directive := f.render("language.md", map[string]string{"Language": language})
	if f.err != nil {
		return "", f.err
	}
	return rules + " " + directive, nil
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
	preamble, err := rulesAndLanguage(paths, lang)
	if err != nil {
		return "", err
	}
	parts := []string{preamble}
	if reportingEnabled() {
		reporting, err := render(paths, "reporting.md", nil)
		if err != nil {
			return "", err
		}
		parts = append(parts, reporting)
	}
	sizeText, err := render(paths, "size.md", map[string]string{"Size": size})
	if err != nil {
		return "", err
	}
	parts = append(parts, sizeText)
	return strings.Join(parts, " "), nil
}

// reportingEnabled reports whether the completion-report reminder belongs in launch prompts.
// An unreadable config leaves it out: a card with LANGUAGE must still launch without a config,
// and the reporting rule itself is read through the config the agent loads first.
func reportingEnabled() bool {
	cfg, err := config.Load(false)
	return err == nil && cfg.Rules[config.RuleReporting]
}

// promptHead renders the one-line head that opens a task file and the argv instruction.
func promptHead(paths config.InstallPaths, kind, taskID, target string, count int) (string, error) {
	return render(paths, "head.md", map[string]any{"Kind": kind, "TaskID": taskID, "Target": target, "Count": count})
}

// taskFileTrailer renders the delete-when-done note appended to every task file.
func taskFileTrailer(paths config.InstallPaths) process.TaskFileTrailer {
	return process.TaskFileTrailerFor(paths.RulesDir)
}

// taskFileInstruction builds the one-line file instruction handed to the agent CLI.
func taskFileInstruction(paths config.InstallPaths, head, path string) (string, error) {
	return process.TaskFileInstruction(paths.RulesDir, head, path)
}

// snippetRef names a phase snippet below the rules root and when the agent reads it.
type snippetRef struct {
	name string
	when string
}

var (
	executeSingleSnippet      = snippetRef{"snippets/execute-single.md", "before executing the card"}
	orchestrateSessionSnippet = snippetRef{"snippets/orchestrate-sessions.md", "before orchestrating"}
)

// phaseSnippets renders the conditional read sentence that every rule entry repeats, followed
// by the phase snippets Kander names for this prompt, each with its absolute path.
func phaseSnippets(paths config.InstallPaths, refs ...snippetRef) string {
	root := "<rules root>"
	if paths.RulesDir != "" {
		root = process.AgentPath(filepath.Clean(paths.RulesDir))
	}
	text := "\n\n" + config.CommandReadSentence(root)
	for _, ref := range refs {
		text += " Read `" + rulesPath(paths, ref.name) + "` " + ref.when + "."
	}
	return text
}

// headedTaskFileInstruction renders the head of kind and the one-line instruction in one call.
func headedTaskFileInstruction(paths config.InstallPaths, kind, taskID, target string, count int, path string) (string, error) {
	head, err := promptHead(paths, kind, taskID, target, count)
	if err != nil {
		return "", err
	}
	return taskFileInstruction(paths, head, path)
}

func startAgentPrompt(taskID string, paths config.InstallPaths, taskGroup, cardText string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	preamble, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	f := &fragments{paths: paths}
	head := f.render("head.md", map[string]any{"Kind": "start", "TaskID": taskID, "Target": "", "Count": 0})
	agents, err := promptAgents(paths)
	if err != nil {
		return "", err
	}
	if f.err != nil {
		return "", f.err
	}
	return render(paths, "start.md", map[string]any{
		"Head":          head,
		"Preamble":      preamble,
		"Command":       commandName(paths),
		"TaskID":        taskID,
		"Agents":        agents,
		"Group":         taskGroup != "",
		"PhaseSnippets": phaseSnippets(paths, executeSingleSnippet),
	})
}

func resumeAgentPrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	return resumePrompt(taskID, message, paths, state, cardText)
}

func promptStatus(paths config.InstallPaths, taskID, state, cardText string) (string, error) {
	if board.MetadataFrom(cardText, "DISPATCH_ID") != "" {
		return render(paths, "dispatch-bound.md", nil)
	}
	return cardStateStatus(paths, taskID, state)
}

// resumeData gathers the fields shared by the resume and takeover bodies.
func resumeData(taskID, message string, paths config.InstallPaths, state, cardText, kind string) (map[string]any, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return nil, launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	preamble, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return nil, err
	}
	head, err := promptHead(paths, kind, taskID, "", 0)
	if err != nil {
		return nil, err
	}
	status, err := promptStatus(paths, taskID, state, cardText)
	if err != nil {
		return nil, err
	}
	agents, err := promptAgents(paths)
	if err != nil {
		return nil, err
	}
	// A card waiting in review/ gets its phase snippet from the dispatch that brings it back;
	// a working card continues or restarts its execution and gets the execution snippet.
	snippets := phaseSnippets(paths)
	if state != "review" {
		snippets = phaseSnippets(paths, executeSingleSnippet)
	}
	return map[string]any{
		"Head":          head,
		"Status":        status,
		"Preamble":      preamble,
		"Command":       commandName(paths),
		"TaskID":        taskID,
		"Message":       message,
		"Agents":        agents,
		"PhaseSnippets": snippets,
	}, nil
}

func resumePrompt(taskID, message string, paths config.InstallPaths, state, cardText string) (string, error) {
	data, err := resumeData(taskID, message, paths, state, cardText, "resume")
	if err != nil {
		return "", err
	}
	return render(paths, "resume.md", data)
}

func takeoverAgentPrompt(taskID, message string, paths config.InstallPaths, previous, state, cardText string) (string, error) {
	data, err := resumeData(taskID, message, paths, state, cardText, "takeover")
	if err != nil {
		return "", err
	}
	data["Previous"] = previous
	return render(paths, "takeover.md", data)
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
