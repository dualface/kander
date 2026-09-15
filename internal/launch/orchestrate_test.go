package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// makeBacklog creates a card that stays in backlog; ready also writes the
// contract and review records the todo gate requires.
func makeBacklog(t *testing.T, root, slug string, ready bool) (string, string) {
	t.Helper()
	path, err := board.NewTask(root, "chore", slug, "任务 "+slug, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(path, "spec.md")
	if ready {
		makeReady(t, document)
	}
	return filepath.Base(path), document
}

func replaceInFile(t *testing.T, path, old, updated string) {
	t.Helper()
	text := mustRead(t, path)
	if !strings.Contains(text, old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(text, old, updated, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func boardStates(t *testing.T, root string) map[string][]string {
	t.Helper()
	states := map[string][]string{}
	for _, state := range board.States {
		entries, err := os.ReadDir(filepath.Join(root, state))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			states[state] = append(states[state], entry.Name())
		}
	}
	return states
}

func TestStartOrchestratorLaunchesSessionWithoutBoardWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	firstID, _ := makeTodo(t, root, "orchestrate-first")
	secondID, _ := makeBacklog(t, root, "orchestrate-second", true)
	before := boardStates(t, root)

	oldCreate := createTaskFile
	var body, prefix, taskFile string
	createTaskFile = func(text, name string) (string, error) {
		body, prefix = text, name
		path, err := oldCreate(text, name)
		taskFile = path
		return path, err
	}
	t.Cleanup(func() { createTaskFile = oldCreate })

	result, err := StartOrchestrator(OrchestrateRequest{
		Root: root, References: []string{secondID, firstID},
		Handover: "Run the second card first; both may run in parallel.", Agent: "claude", Launcher: "tmux",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent != "claude" || result.Launcher != "tmux" || !strings.Contains(result.Address, ":") {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Tasks) != 2 || result.Tasks[0].TaskID != secondID || result.Tasks[0].State != "backlog" || result.Tasks[1].TaskID != firstID {
		t.Fatalf("tasks lost the plan order: %+v", result.Tasks)
	}
	if _, err := os.Stat(taskFile); err != nil {
		t.Fatalf("task file handed to the agent must stay: %v", err)
	}
	if !strings.HasPrefix(prefix, "kander-orchestrate-"+secondID+"-") {
		t.Fatalf("prefix=%s", prefix)
	}
	for _, want := range []string{secondID, firstID, "Run the second card first", "Orchestrator Sessions", `"en"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, secondID) > strings.Index(body, firstID) {
		t.Fatalf("prompt lost the plan order:\n%s", body)
	}
	if args := mustRead(t, filepath.Join(root, "tmux.log")); !strings.Contains(args, "orchestrate-"+secondID) {
		t.Fatalf("tmux=%s", args)
	}
	if !strings.Contains(lastCommand(t, root), "claude") {
		t.Fatalf("command=%s", lastCommand(t, root))
	}
	after := boardStates(t, root)
	for _, state := range board.States {
		if strings.Join(before[state], ",") != strings.Join(after[state], ",") {
			t.Fatalf("orchestrate changed board state %s: %v -> %v", state, before[state], after[state])
		}
	}
}

func TestStartOrchestratorLaunchesSingleCard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	for _, state := range []string{"backlog", "todo"} {
		t.Run(state, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			loadEffective = func() (*config.Config, error) {
				return envConfig("codex", "tmux", map[string]string{"large": "grok", "small": "codex"}), nil
			}
			var id, document string
			if state == "backlog" {
				id, document = makeBacklog(t, root, "orchestrate-single", true)
			} else {
				id, document = makeTodo(t, root, "orchestrate-single")
			}
			before := mustRead(t, document)
			result, err := StartOrchestrator(OrchestrateRequest{
				Root: root, References: []string{id}, Handover: "Start and monitor this card until it is done.",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Agent != "grok" || result.Launcher != "tmux" || !strings.Contains(result.Address, ":") {
				t.Fatalf("result=%+v", result)
			}
			if len(result.Tasks) != 1 || result.Tasks[0] != (OrchestrateTask{TaskID: id, State: state}) {
				t.Fatalf("tasks=%+v", result.Tasks)
			}
			if after := mustRead(t, document); after != before {
				t.Fatal("starting the orchestrator changed the card")
			}
		})
	}
}

func TestStartOrchestratorUsesTheLargeAgentAndExpandsTaskGroups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "tmux", map[string]string{"large": "grok", "small": "codex"}), nil
	}
	group := time.Now().Format("20060102") + "-orchestrate-group"
	for _, slug := range []string{"orchestrate-member-a", "orchestrate-member-b"} {
		_, document := makeBacklog(t, root, slug, true)
		replaceInFile(t, document, "- TASK_GROUP:", "- TASK_GROUP: "+group)
	}
	result, err := StartOrchestrator(OrchestrateRequest{
		Root: root, References: []string{group}, Handover: "Orchestrate the group.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent != "grok" || len(result.Tasks) != 2 || result.Tasks[0].TaskGroup != group {
		t.Fatalf("result=%+v", result)
	}
}

func TestStartOrchestratorRejectsInvalidTaskSets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	readyID, _ := makeTodo(t, root, "orchestrate-ready")
	otherID, _ := makeTodo(t, root, "orchestrate-other")
	draftID, _ := makeBacklog(t, root, "orchestrate-draft", false)
	startedID, _ := makeTodo(t, root, "orchestrate-started")
	loaded, err := board.LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := board.MoveEntry(loaded.Entries[startedID], root, "working"); err != nil {
		t.Fatal(err)
	}
	emptyGroup := time.Now().Format("20060102") + "-empty-orchestrate-group"
	cases := []struct {
		name       string
		references []string
		want       string
	}{
		{"empty", nil, "需要至少一张任务卡"},
		{"repeated", []string{readyID, otherID, readyID}, "被重复指定"},
		{"started", []string{startedID}, "位于 working"},
		{"draft", []string{draftID}, "尚不能进入 todo"},
		{"group", []string{readyID, emptyGroup}, "没有成员卡"},
		{"missing", []string{readyID, time.Now().Format("20060102") + "-missing-task"}, "任务不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := StartOrchestrator(OrchestrateRequest{Root: root, References: tc.references, Handover: "plan"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	if _, err := StartOrchestrator(OrchestrateRequest{Root: root, References: []string{readyID, otherID}}); err == nil {
		t.Fatal("an empty handover must be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log")); err == nil {
		t.Fatal("a rejected task set must not create a container")
	}
}

func TestStartOrchestratorFailureClosesTheContainerAndTaskFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	firstID, _ := makeTodo(t, root, "orchestrate-fail-first")
	secondID, _ := makeTodo(t, root, "orchestrate-fail-second")
	oldCreate := createTaskFile
	var taskFile string
	createTaskFile = func(text, name string) (string, error) {
		path, err := oldCreate(text, name)
		taskFile = path
		return path, err
	}
	t.Cleanup(func() { createTaskFile = oldCreate })
	t.Setenv("KANBAN_TMUX_RESPAWN_FAIL", "1")

	if _, err := StartOrchestrator(OrchestrateRequest{
		Root: root, References: []string{firstID, secondID}, Handover: "plan", Agent: "claude", Launcher: "tmux",
	}); err == nil {
		t.Fatal("expected the launch to fail")
	}
	if kill, err := os.ReadFile(filepath.Join(root, "tmux.log.kill")); err != nil || !strings.Contains(string(kill), "kill-window") {
		t.Fatalf("created window was not closed: %q %v", kill, err)
	}
	if _, err := os.Stat(taskFile); taskFile == "" || !os.IsNotExist(err) {
		t.Fatalf("failed launch left task file %q", taskFile)
	}
}

func TestOrchestratorPromptFallsBackToConfiguredLanguageWhenCardsDisagree(t *testing.T) {
	setupBoard(t)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	tasks := []OrchestrateTask{{TaskID: "a"}, {TaskID: "b"}}
	body, err := orchestratorAgentPrompt(tasks, []string{"- LANGUAGE: ja\n", "- LANGUAGE: ja\n"}, "plan", paths)
	if err != nil || !strings.Contains(body, `"ja"`) {
		t.Fatalf("shared language: err=%v\n%s", err, body)
	}
	body, err = orchestratorAgentPrompt(tasks, []string{"- LANGUAGE: ja\n", "- LANGUAGE: en\n"}, "plan", paths)
	if err != nil || !strings.Contains(body, `"zh-CN"`) {
		t.Fatalf("mixed languages must use the configured agent language: err=%v\n%s", err, body)
	}
}

func TestOrchestratorWindowNameIsBounded(t *testing.T) {
	if name := orchestratorWindowName("20260913-short-task"); name != "orchestrate-20260913-short-task" {
		t.Fatalf("name=%s", name)
	}
	if name := orchestratorWindowName("20260913-" + strings.Repeat("a", 80) + "-task"); len([]rune(name)) > 50 || strings.HasSuffix(name, "-") {
		t.Fatalf("name=%s", name)
	}
}

func TestRunOrchestrateRequiresTasksAndHandover(t *testing.T) {
	setupBoard(t)
	if code := RunOrchestrate([]string{"--message", "plan"}); code != 2 {
		t.Fatalf("missing tasks: code=%d", code)
	}
	if code := RunOrchestrate([]string{"20260913-a-task", "20260913-b-task"}); code == 0 {
		t.Fatal("missing handover must fail")
	}
	out, _, err := capture(t, func() error {
		if code := RunOrchestrate([]string{"--help"}); code != 0 {
			return launchError("help exit "+itoa(code), "help exit "+itoa(code))
		}
		return nil
	})
	if err != nil || !strings.Contains(out, "--message-file") {
		t.Fatalf("help=%s err=%v", out, err)
	}
}
