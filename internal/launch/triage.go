package launch

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/issue"
)

// TriagePreview contains the configured execution agent and launcher an
// unstarted takeover would use. The TUI resolves it before confirming a
// session, so it can point the user at the CLI for launchers that need the
// terminal of the caller.
type TriagePreview struct {
	Agent    string
	Launcher string
}

// PreviewTriage resolves the configured small-tier agent and launcher without
// allocating a session or creating a container. A card does not exist yet, so
// the small tier is the documented default of the takeover entry.
func PreviewTriage(agentOverride, launcherOverride string) (TriagePreview, error) {
	cfg, err := loadEffective()
	if err != nil {
		return TriagePreview{}, err
	}
	agent, launcher, err := triageDefaults(cfg, agentOverride, launcherOverride)
	if err != nil {
		return TriagePreview{}, err
	}
	return TriagePreview{Agent: agent, Launcher: launcher}, nil
}

// StartTriage starts one takeover session for a prepared issue. It reads and
// writes no board state: the evidence files were written by the caller, the
// card is created by the takeover agent after the user agrees, and the card
// metadata of the target project is never touched. A failed start closes the
// container it created and removes its temporary task file.
func StartTriage(request issue.TriageLaunch) (result issue.TriageOutcome, err error) {
	var warnings board.WarningLog
	defer func() { result.Warnings = append(warnings.Messages(), result.Warnings...) }()

	if err := request.Repository.Validate(); err != nil {
		return result, err
	}
	if request.Number <= 0 {
		return result, issue.NewError(issue.ErrorInvalidQuery, "number", strconv.Itoa(request.Number))
	}
	if strings.TrimSpace(request.Root) == "" {
		return result, launchError("launch.board_root_is_required")
	}
	if err := validateTriageEvidence(request); err != nil {
		return result, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	agent, launcher, err := triageDefaults(cfg, request.Agent, request.Launcher)
	if err != nil {
		return result, err
	}
	plan, err := prepareLaunch(launcher, parentDir(request.Root), "triage")
	if err != nil {
		return result, err
	}
	if err := applyAgentDelivery(&plan, cfg, agent); err != nil {
		return result, err
	}
	plan.warning = func(message string) { result.Warnings = append(result.Warnings, message) }
	program, err := requireAgentProgram(agent, cfg)
	if err != nil {
		return result, err
	}
	session, err := newAgentSession(agent, program, cfg)
	if err != nil {
		return result, err
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return result, err
	}
	body, err := triageAgentPrompt(request, paths)
	if err != nil {
		return result, err
	}
	prefix := "kander-issue-" + issue.TriageDirectoryName(request.Repository, request.Number) + "-triage-"
	taskFile, err := createTaskFile(body, prefix)
	if err != nil {
		return result, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.triage_head", triageTarget(request.Repository, request.Number)), taskFile)
	args, err := agentArguments(agent, cfg.Models.Kanban[agent], "small", session, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	// A takeover session owns no card, so it records no pane session marker and
	// reports no Herdr agent session; those identities belong to card launches.
	outcome, err := launchAgent(plan, request.Root, triageWindowName(request.Repository, request.Number), inv, nil, nil, nil)
	if err != nil {
		return result, err
	}
	handedOff = true
	if plan.Launcher == "foreground" {
		code, waitErr := outcome.Wait()
		if waitErr != nil {
			return result, launchError("launch.failed_to_start_agent", waitErr.Error())
		}
		if code != 0 {
			return result, launchError("launch.triage_agent_exited_with_status", itoa(code))
		}
	}
	result.Agent, result.Launcher = agent, plan.Launcher
	result.Address = triageAddress(plan, outcome)
	return result, nil
}

// triageDefaults resolves the takeover agent and launcher. Overrides win over
// the configuration; the small tier is the default because no card exists yet.
func triageDefaults(cfg *config.Config, agentOverride, launcherOverride string) (string, string, error) {
	agent := strings.TrimSpace(agentOverride)
	if agent == "" {
		var err error
		agent, err = config.KanbanAgentFor(cfg, "small")
		if err != nil {
			return "", "", err
		}
	}
	if !config.HasAgent(cfg, agent) {
		return "", "", launchError("launch.unsupported_agent", agent)
	}
	launcher := strings.TrimSpace(launcherOverride)
	if launcher == "" {
		launcher = cfg.Launcher
	}
	if !contains(config.Launchers, launcher) {
		return "", "", launchError("launch.unknown_launcher", launcher)
	}
	resolved, err := resolveStartLauncher(launcher)
	if err != nil {
		return "", "", err
	}
	return agent, resolved, nil
}

// validateTriageEvidence refuses to start a session whose prompt would point at
// evidence files that are missing or empty.
func validateTriageEvidence(request issue.TriageLaunch) error {
	for _, path := range []string{request.JSONPath, request.MarkdownPath} {
		if strings.TrimSpace(path) == "" {
			return launchError("launch.triage_evidence_is_missing")
		}
		data, found, err := fs.ReadRegularFileIfExists(request.Root, path)
		if err != nil || !found || len(data) == 0 {
			return launchError("launch.triage_evidence_is_missing")
		}
	}
	return nil
}

// triageAgentPrompt builds the takeover prompt from local paths and the
// confirmed identity only. The remote title, body and comments never enter the
// prompt; the agent reads them from the evidence files as untrusted data.
func triageAgentPrompt(request issue.TriageLaunch, paths config.InstallPaths) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	lang, err := resolvePromptLanguage("", paths)
	if err != nil {
		return "", err
	}
	rules := RuleLoadingInstruction(paths) + promptLanguageDirective(lang) + " "
	issueRules := filepath.Join(paths.RulesDir, "KANDER-ISSUE-RULES.md")
	// The import instruction belongs to the unbound branch only: a session
	// started for a bound card continues that card and must never be told to
	// import the issue again.
	cardLine := t("launch.prompt.triage_no_card", commandName(paths), strconv.Itoa(request.Number))
	if request.CardID != "" {
		cardLine = t("launch.prompt.triage_with_card", request.CardID)
	}
	return t(
		"launch.prompt.triage",
		triageTarget(request.Repository, request.Number),
		rules,
		request.JSONPath,
		request.MarkdownPath,
		cardLine,
		promptAgents(paths),
		issueRules,
	), nil
}

// triageTarget renders the confirmed identity of one issue from the validated
// repository fields and the issue number.
func triageTarget(repository issue.Repository, number int) string {
	return repository.Owner + "/" + repository.Name + "#" + strconv.Itoa(number)
}

// triageWindowName builds the container name of a takeover session:
// issue-<owner>-<repo>-<number>. The name is reduced to the task-ID alphabet
// and capped at 50 runes with the issue number kept visible, so a very long
// identity can never create an oversized or ambiguous window name.
func triageWindowName(repository issue.Repository, number int) string {
	name := issue.TriageDirectoryName(repository, number)
	suffix := "-" + strconv.Itoa(number)
	if len([]rune("issue-"+name)) <= 50 {
		return "issue-" + name
	}
	head := strings.TrimSuffix(name, suffix)
	budget := 50 - len([]rune("issue-"+suffix))
	runes := []rune(head)
	if len(runes) > budget {
		runes = runes[:budget]
	}
	head = strings.TrimRight(string(runes), "-")
	return "issue-" + head + suffix
}

// triageAddress renders the container address of one started takeover session.
// Foreground and console sessions have no container address.
func triageAddress(plan LaunchPlan, outcome LaunchOutcome) string {
	switch plan.Launcher {
	case "herdr":
		return outcome.Tab + ":" + outcome.Pane
	case "tmux", "tmux-session":
		return plan.Session + ":" + outcome.Window + ":" + outcome.Pane
	default:
		return ""
	}
}
