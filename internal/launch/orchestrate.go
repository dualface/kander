package launch

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// orchestrateGroupRe mirrors the board's task group ID grammar; a reference that
// matches it expands to the group's current members instead of naming a card.
var orchestrateGroupRe = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

// OrchestrateRequest names the plan a creating session hands to a separate
// orchestrator session. References keep the confirmed plan order.
type OrchestrateRequest struct {
	Root       string
	References []string
	Handover   string
	Agent      string
	Launcher   string
}

// OrchestrateTask is one card the orchestrator session receives.
type OrchestrateTask struct {
	TaskID    string
	State     string
	TaskGroup string
}

// OrchestrateResult describes one started orchestrator session.
type OrchestrateResult struct {
	Agent    string
	Launcher string
	Address  string
	Tasks    []OrchestrateTask
	Warnings []string
}

// StartOrchestrator starts one orchestrator session for one or more cards. It
// writes no board state: the orchestrator picks and starts the cards itself
// under the rules. A failed start closes the container it created and removes
// its temporary task file.
func StartOrchestrator(request OrchestrateRequest) (result OrchestrateResult, err error) {
	var warnings board.WarningLog
	defer func() { result.Warnings = append(warnings.Messages(), result.Warnings...) }()

	if strings.TrimSpace(request.Handover) == "" {
		return result, launchError("launch.message_must_not_be_empty", "orchestrate")
	}
	loaded, err := board.LoadBoardWithWarnings(request.Root, &warnings)
	if err != nil {
		return result, err
	}
	tasks, texts, err := resolveOrchestrateTasks(loaded, request.References)
	if err != nil {
		return result, err
	}
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	for i, task := range tasks {
		if err := cfg.Rules.CheckTaskGroup(task.TaskGroup); err != nil {
			return result, err
		}
		if task.State == "backlog" {
			if err := board.ValidateTodoContract(loaded.Entries[task.TaskID].Kind, texts[i]); err != nil {
				return result, launchError("orchestrate.task_not_ready", task.TaskID, err.Error())
			}
		}
	}
	// Orchestration may span several cards and review batches, so the session
	// uses the large-tier agent even for one card unless the caller overrides it.
	agent, launcher, err := sessionDefaults(cfg, "large", request.Agent, request.Launcher)
	if err != nil {
		return result, err
	}
	plan, err := prepareLaunch(launcher, parentDir(request.Root), "orchestrate")
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
	body, err := orchestratorAgentPrompt(tasks, texts, request.Handover, paths)
	if err != nil {
		return result, err
	}
	taskFile, err := createTaskFile(body, "kander-orchestrate-"+tasks[0].TaskID+"-")
	if err != nil {
		return result, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("orchestrate.prompt_head", strconv.Itoa(len(tasks))), taskFile)
	args, err := agentArguments(agent, cfg.Models.Kanban[agent], "large", session, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	// The orchestrator owns no card, so it records no pane session marker and
	// reports no Herdr agent session; those identities belong to card launches.
	outcome, err := launchAgent(plan, request.Root, orchestratorWindowName(tasks[0].TaskID), inv, nil, nil, nil)
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
			return result, launchError("orchestrate.agent_exited_with_status", itoa(code))
		}
	}
	result.Agent, result.Launcher, result.Tasks = agent, plan.Launcher, tasks
	result.Address = OpaqueAddress(plan, outcome)
	return result, nil
}

// resolveOrchestrateTasks expands references in plan order, with a group's
// members in the board's task ID order, and rejects unknown, repeated, or
// already started cards. Group expansion requires a complete membership view,
// so a hidden member can never be skipped.
func resolveOrchestrateTasks(loaded board.Board, references []string) ([]OrchestrateTask, []string, error) {
	var membership board.Membership
	for _, reference := range references {
		if orchestrateGroupRe.MatchString(reference) {
			membership = loaded.GroupMembership()
			if err := membership.Err(); err != nil {
				return nil, nil, err
			}
			break
		}
	}
	var ids []string
	for _, reference := range references {
		if orchestrateGroupRe.MatchString(reference) {
			members := membership.Groups[reference]
			if len(members) == 0 {
				return nil, nil, launchError("orchestrate.task_group_has_no_members", reference)
			}
			ids = append(ids, members...)
			continue
		}
		entry, err := board.Locate(loaded, reference)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, entry.TaskID)
	}
	if len(ids) == 0 {
		return nil, nil, launchError("orchestrate.requires_at_least_one_task")
	}
	seen := map[string]bool{}
	tasks := make([]OrchestrateTask, 0, len(ids))
	texts := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, nil, launchError("orchestrate.task_repeated", id)
		}
		seen[id] = true
		entry := loaded.Entries[id]
		if entry.State != "backlog" && entry.State != "todo" {
			return nil, nil, launchError("orchestrate.task_already_started", id, entry.State)
		}
		text, err := readDocumentFn(entry)
		if err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, OrchestrateTask{TaskID: id, State: entry.State, TaskGroup: board.TaskGroupFrom(text)})
		texts = append(texts, text)
	}
	return tasks, texts, nil
}

// orchestratorAgentPrompt lists the handed-over cards and the creator's plan
// notes. The communication language is the cards' shared LANGUAGE, or the
// configured agent language when the cards disagree or carry none.
func orchestratorAgentPrompt(tasks []OrchestrateTask, texts []string, handover string, paths config.InstallPaths) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	shared := strings.TrimSpace(metadataFrom(texts[0], board.FieldLanguage))
	for _, text := range texts[1:] {
		if strings.TrimSpace(metadataFrom(text, board.FieldLanguage)) != shared {
			shared = ""
			break
		}
	}
	lang := shared
	if lang == "" {
		var err error
		if lang, err = resolvePromptLanguage("", paths); err != nil {
			return "", err
		}
	}
	var list strings.Builder
	for _, task := range tasks {
		group := task.TaskGroup
		if group == "" {
			group = t("orchestrate.no_task_group")
		}
		fmt.Fprintf(&list, "- %s (%s, %s)\n", task.TaskID, task.State, group)
	}
	rules := RuleLoadingInstruction(paths) + promptLanguageDirective(lang) + " "
	return t(
		"orchestrate.prompt",
		rules,
		strings.TrimRight(list.String(), "\n"),
		handover,
		promptAgents(paths),
	), nil
}

// orchestratorWindowName names the container after the first card in plan
// order, capped at 50 runes like every other launch container name.
func orchestratorWindowName(firstTaskID string) string {
	name := []rune("orchestrate-" + firstTaskID)
	if len(name) > 50 {
		name = name[:50]
	}
	return strings.TrimRight(string(name), "-")
}

// RunOrchestrate implements kander orchestrate.
func RunOrchestrate(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprintln(os.Stdout, t("orchestrate.usage"))
			return 0
		}
	}
	rest, agent, launcher, _, message := parseAgentLauncher(args)
	if message != "" {
		return orchestrateUsageFail(message)
	}
	var handover, handoverFile string
	var handoverSet bool
	var references []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "--message":
			val, next, ok := takeValue(rest, i)
			if !ok {
				return orchestrateUsageFail(t("launch.missing_message_value"))
			}
			handover, handoverSet, i = val, true, next
		case strings.HasPrefix(arg, "--message="):
			handover, handoverSet = strings.TrimPrefix(arg, "--message="), true
		case arg == "--message-file":
			val, next, ok := takeValue(rest, i)
			if !ok {
				return orchestrateUsageFail(t("launch.missing_message_file_value"))
			}
			handoverFile, i = val, next
		case strings.HasPrefix(arg, "--message-file="):
			handoverFile = strings.TrimPrefix(arg, "--message-file=")
		case strings.HasPrefix(arg, "-"):
			return orchestrateUsageFail(t("board.unknown_option", arg))
		default:
			references = append(references, arg)
		}
	}
	if len(references) == 0 {
		return orchestrateUsageFail(t("orchestrate.requires_at_least_one_task"))
	}
	text, err := readTaskMessage(handover, handoverSet, handoverFile, "orchestrate")
	if err != nil {
		return fail(err)
	}
	if _, err := config.Load(false); err != nil {
		return fail(err)
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	result, err := StartOrchestrator(OrchestrateRequest{
		Root: root, References: references, Handover: text, Agent: agent, Launcher: launcher,
	})
	for _, warning := range result.Warnings {
		fmt.Fprint(os.Stderr, warning)
	}
	if err != nil {
		return fail(err)
	}
	if result.Address == "" {
		fmt.Println(t("orchestrate.started_no_address", result.Agent, result.Launcher, strconv.Itoa(len(result.Tasks))))
	} else {
		fmt.Println(t("orchestrate.started", result.Agent, result.Launcher, strconv.Itoa(len(result.Tasks)), result.Address))
	}
	return 0
}

func orchestrateUsageFail(message string) int {
	fmt.Fprintln(os.Stderr, t("orchestrate.usage"))
	fmt.Fprintln(os.Stderr, message)
	return 2
}
