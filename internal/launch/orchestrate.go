package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// orchestrateGroupRe mirrors the board's task group ID grammar; a reference that
// matches it expands to the group's current members instead of naming a card.
var orchestrateGroupRe = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

// OrchestrateRequest names the plan a creating session hands to a separate
// orchestrator session. References keep the confirmed plan order.
type OrchestrateRequest struct {
	requireService bool
	Root           string
	References     []string
	Handover       string
	Agent          string
	Launcher       string
	OperationID    string
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
	cfg, err := loadEffective()
	if err != nil {
		return result, err
	}
	if request.requireService {
		if err := remote.RequireBinding(cfg); err != nil {
			return result, err
		}
	}
	var loaded board.Board
	var service *remote.Session
	if remote.ServiceSelected(cfg, request.requireService) {
		ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
		loaded, service, request.Root, err = remoteOrchestrateBoard(ctx, cfg)
		cancel()
	} else {
		loaded, err = board.LoadBoardWithWarnings(request.Root, &warnings)
	}
	if err != nil {
		return result, err
	}
	var journal *remote.ActionJournal
	if service != nil {
		var fresh bool
		journal, fresh, err = service.BeginAction("orchestrate", struct {
			References                             []string
			Handover, Agent, Launcher, OperationID string
		}{request.References, request.Handover, request.Agent, request.Launcher, request.OperationID})
		if err != nil {
			return result, err
		}
		defer journal.Close()
		if !fresh && journal.Value.Phase != "prepared" && journal.Value.Phase != "session-ready" {
			if journal.Value.Phase == "complete" && json.Unmarshal(journal.Value.Result, &result) == nil {
				return result, nil
			}
			return result, launchError("remote.launch_reconciliation_required", journal.Path)
		}
	}
	tasks, texts, err := resolveOrchestrateTasks(loaded, request.References)
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
	session, err := journalAgentSession(journal, agent, program, cfg)
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
	taskFile, err := createTaskFile(body, "kander-orchestrate-"+tasks[0].TaskID+"-", taskFileTrailer(paths))
	if err != nil {
		return result, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt, err := headedTaskFileInstruction(paths, "orchestrate", "", "", len(tasks), taskFile)
	if err != nil {
		return result, err
	}
	args, err := agentArguments(agent, cfg.Models.Kanban[agent], "large", session, false, cfg)
	if err != nil {
		return result, err
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, prompt))
	if err != nil {
		return result, err
	}
	var location func(LaunchOutcome) error
	var paneSession func() (AgentSession, error)
	if service != nil {
		inv, location, paneSession, err = bindRemoteOrchestrator(&plan, inv, &session, service, program, tasks[0].TaskID, parentDir(request.Root), journal)
		if err != nil {
			return result, err
		}
	}
	if journal != nil {
		journal.Value.TaskFile, journal.Value.Phase = taskFile, "launching"
		if err := journal.Save(); err != nil {
			return result, err
		}
		plan.beforePrompt = func() error { journal.Value.Phase = "prompt-attempted"; return journal.Save() }
		plan.judge = func() launchVerdict { return verdictKeep }
	}
	// Local legacy orchestration has no card identity. Distributed sessions keep
	// their identity in private launcher context for later coordinator leases.
	outcome, err := launchAgent(plan, request.Root, orchestratorWindowName(tasks[0].TaskID), inv, location, paneSession, &session, service != nil)
	if err != nil {
		if journal != nil {
			handedOff = true
			return result, errors.Join(err, launchError("remote.launch_reconciliation_required", journal.Path))
		}
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
	if journal != nil {
		journal.Value.Phase = "complete"
		journal.Value.Result, err = json.Marshal(result)
		if err == nil {
			err = journal.Save()
		}
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// resolveOrchestrateTasks expands references in plan order, with a group's
// members in the board's task ID order, and rejects unknown, repeated, or
// already started cards. Group expansion requires a complete membership view of
// the referenced groups, so a hidden member can never be skipped; a broken card
// that provably belongs to no referenced group does not block the plan.
func resolveOrchestrateTasks(loaded board.Board, references []string) ([]OrchestrateTask, []string, error) {
	var membership board.Membership
	var referencedGroups []string
	for _, reference := range references {
		if orchestrateGroupRe.MatchString(reference) {
			referencedGroups = append(referencedGroups, reference)
		}
	}
	if len(referencedGroups) > 0 {
		membership = loaded.GroupMembership()
		if err := membership.ErrFor(referencedGroups...); err != nil {
			return nil, nil, err
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
		text, err := loaded.Document(id)
		if err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, OrchestrateTask{TaskID: id, State: entry.State, TaskGroup: board.TaskGroupFrom(text)})
		texts = append(texts, text)
	}
	return tasks, texts, nil
}

// orchestrateLanguage is the communication language of an orchestrator session: the cards'
// shared LANGUAGE, or the configured agent language when the cards disagree or carry none.
func orchestrateLanguage(texts []string, paths config.InstallPaths) (string, error) {
	shared := strings.TrimSpace(metadataFrom(texts[0], board.FieldLanguage))
	for _, text := range texts[1:] {
		if strings.TrimSpace(metadataFrom(text, board.FieldLanguage)) != shared {
			shared = ""
			break
		}
	}
	if shared != "" {
		return shared, nil
	}
	return resolvePromptLanguage("", paths)
}

// orchestratorAgentPrompt lists the handed-over cards and the creator's plan
// notes in the language orchestrateLanguage resolves.
func orchestratorAgentPrompt(tasks []OrchestrateTask, texts []string, handover string, paths config.InstallPaths) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	lang, err := orchestrateLanguage(texts, paths)
	if err != nil {
		return "", err
	}
	var list strings.Builder
	for _, task := range tasks {
		group := task.TaskGroup
		if group == "" {
			group = "no task group"
		}
		fmt.Fprintf(&list, "- %s (%s, %s)\n", task.TaskID, task.State, group)
	}
	preamble, err := rulesAndLanguage(paths, lang)
	if err != nil {
		return "", err
	}
	agents, err := promptAgents(paths)
	if err != nil {
		return "", err
	}
	return render(paths, "orchestrate.md", map[string]any{
		"Preamble":      preamble,
		"Tasks":         strings.TrimRight(list.String(), "\n"),
		"Handover":      handover,
		"Agents":        agents,
		"PhaseSnippets": phaseSnippets(paths, orchestrateSnippets(tasks)...),
	})
}

// orchestrateSnippets names the orchestrator snippet and, when the plan holds a task group
// member, the group start snippet.
func orchestrateSnippets(tasks []OrchestrateTask) []snippetRef {
	refs := []snippetRef{orchestrateSessionSnippet}
	for _, task := range tasks {
		if task.TaskGroup != "" {
			return append(refs, orchestrateStartSnippet)
		}
	}
	return refs
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
func RunOrchestrate(args []string) int { return runServiceOrchestrate(args, false) }

// RunOrchestrateRemote is the mandatory REST entrance used by the client binary.
func RunOrchestrateRemote(args []string) int { return runServiceOrchestrate(args, true) }

func runServiceOrchestrate(args []string, required bool) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprintln(os.Stdout, config.CommandUsage("orchestrate"))
			return 0
		}
	}
	rest, agent, launcher, _, message := parseAgentLauncher(args)
	if message != "" {
		return orchestrateUsageFail(message)
	}
	var handover, handoverFile, operationID string
	var handoverSet bool
	var references []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "--operation-id":
			val, next, ok := takeValue(rest, i)
			if !ok || !api.ValidIdentity(val) {
				return orchestrateUsageFail(t("remote.launch_operation_id_invalid"))
			}
			operationID, i = val, next
		case strings.HasPrefix(arg, "--operation-id="):
			operationID = strings.TrimPrefix(arg, "--operation-id=")
			if !api.ValidIdentity(operationID) {
				return orchestrateUsageFail(t("remote.launch_operation_id_invalid"))
			}
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
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if required {
		if err := remote.RequireBinding(cfg); err != nil {
			return fail(err)
		}
	}
	var root string
	if remote.ServiceSelected(cfg, required) {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(err)
		}
		root = filepath.Join(remote.ProjectDir(cwd), "task-context")
	} else {
		root, err = boardRootFn()
		if err != nil {
			return fail(err)
		}
	}
	result, err := StartOrchestrator(OrchestrateRequest{
		Root: root, requireService: required, OperationID: operationID, References: references, Handover: text, Agent: agent, Launcher: launcher,
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
	fmt.Fprintln(os.Stderr, config.CommandUsage("orchestrate"))
	fmt.Fprintln(os.Stderr, message)
	return 2
}
