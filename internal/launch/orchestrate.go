package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// orchestrateGroupRe mirrors the board's task group ID grammar; a reference that
// matches it expands to the group's current members instead of naming a card.
var orchestrateGroupRe = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

// OrchestrateRequest names the plan a creating session hands to a separate
// orchestrator session. References keep the confirmed plan order.
type OrchestrateRequest struct {
	// Root is the directory the session starts in; StartOrchestrator takes it
	// from the server session of the working directory.
	Root        string
	References  []string
	Handover    string
	Agent       string
	Launcher    string
	OperationID string
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
func StartOrchestrator(request OrchestrateRequest) (OrchestrateResult, error) {
	cfg, err := loadEffective()
	if err != nil {
		return OrchestrateResult{}, err
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return OrchestrateResult{}, err
	}
	// A launcher that takes the terminal is refused before the server session of this host is
	// opened (which creates the client identity and registers the project) when this project
	// holds no receipt of this orchestrator session that a call would replay. Otherwise the
	// receipt decides.
	if err := refuseBeforeSession(cfg, request.Launcher, "orchestrate", true, orchestrateReceiptInput{request.References, request.Handover, request.Agent, request.Launcher, request.OperationID}); err != nil {
		return OrchestrateResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
	loaded, service, root, err := remoteOrchestrateBoard(ctx, cfg)
	cancel()
	if err != nil {
		return OrchestrateResult{}, err
	}
	request.Root = root
	return startOrchestrator(request, cfg, loaded, service)
}

// orchestrateReceiptInput is the logical input of the receipt of one orchestrator session.
type orchestrateReceiptInput struct {
	References                             []string
	Handover, Agent, Launcher, OperationID string
}

// startOrchestrator validates the plan against the loaded board and starts the
// session. service is the server session the board came from.
func startOrchestrator(request OrchestrateRequest, cfg *config.Config, loaded board.Board, service *remote.Session) (result OrchestrateResult, err error) {
	if strings.TrimSpace(request.Handover) == "" {
		return result, launchError("launch.message_must_not_be_empty", "orchestrate")
	}
	var journal *remote.ActionJournal
	if service == nil {
		// Without a receipt every call starts a session.
		if err := gateLaunch(cfg, request.Launcher, true); err != nil {
			return result, err
		}
	}
	if service != nil {
		var fresh bool
		logical := orchestrateReceiptInput{request.References, request.Handover, request.Agent, request.Launcher, request.OperationID}
		var current *remote.LocalAction
		if stored, found, err := service.PeekAction("orchestrate", logical); err != nil {
			return result, err
		} else if found {
			current = &stored
		}
		if err := gateLaunch(cfg, request.Launcher, launchWouldStart(current, true)); err != nil {
			return result, err
		}
		journal, fresh, err = service.BeginActionGated("orchestrate", logical, true, actionGate(cfg, request.Launcher, true))
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

// NewOrchestrateCommand builds kander orchestrate: it hands a plan to a separate orchestrator
// session against the server.
func NewOrchestrateCommand() *cobra.Command {
	var agent, launcher, handover, handoverFile, operationID string
	c := command.RequireProject(&cobra.Command{
		Use:  "orchestrate <task-id|task-group-id>...",
		Args: command.Args(1, -1),
	})
	c.PreRunE = func(c *cobra.Command, _ []string) error { return RequireMessage(c, "orchestrate") }
	c.RunE = func(_ *cobra.Command, references []string) error {
		return command.Exit(runOrchestrate(OrchestrateRequest{OperationID: operationID, References: references, Agent: agent, Launcher: launcher}, handover, c.Flags().Changed("message"), handoverFile))
	}
	AddAgentFlags(c, &agent, &launcher)
	command.Checked(c.Flags(), &operationID, "operation-id", "", func(id string) error {
		if !api.ValidIdentity(id) {
			return command.Usage("remote.launch_operation_id_invalid")
		}
		return nil
	})
	c.Flags().StringVar(&handover, "message", "", "")
	c.Flags().StringVar(&handoverFile, "message-file", "", "")
	return c
}

func runOrchestrate(request OrchestrateRequest, handover string, handoverSet bool, handoverFile string) int {
	text, err := readTaskMessage(handover, handoverSet, handoverFile, "orchestrate")
	if err != nil {
		return fail(err)
	}
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	request.Handover = text
	result, err := StartOrchestrator(request)
	for _, warning := range result.Warnings {
		command.Notify("launch_warning", warning)
	}
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		tasks := make([]string, 0, len(result.Tasks))
		for _, task := range result.Tasks {
			tasks = append(tasks, task.TaskID)
		}
		return command.Result(struct {
			Agent    string   `json:"agent"`
			Launcher string   `json:"launcher"`
			Address  string   `json:"address"`
			Tasks    []string `json:"tasks"`
		}{result.Agent, result.Launcher, result.Address, tasks})
	}
	if result.Address == "" {
		fmt.Println(t("orchestrate.started_no_address", result.Agent, result.Launcher, strconv.Itoa(len(result.Tasks))))
	} else {
		fmt.Println(t("orchestrate.started", result.Agent, result.Launcher, strconv.Itoa(len(result.Tasks)), result.Address))
	}
	return 0
}
