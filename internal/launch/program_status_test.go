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
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/process"
)

// writeFakeHC installs an hc that records every call; the entrance tests
// prove that a refused agent never reaches it.
func writeFakeHC(t *testing.T, bin, root string) string {
	t.Helper()
	log := filepath.Join(root, "hc.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + process.AgentPath(log) + "\nprintf '{}\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

// plainAgent is a custom agent without an OSC 7501 declaration.
func plainAgent(bin string) config.AgentDefinition {
	return config.AgentDefinition{
		Path:    filepath.Join(bin, "claude"),
		Args:    &config.AgentArgs{Start: []string{"--interactive"}, Resume: []string{"--interactive", "--resume", "{session=}"}},
		Session: &config.AgentSessionDefinition{Mode: "generated"},
	}
}

// programStatusFixture prepares a board whose configuration knows the
// undeclared agent plaincli; launcher picks the configured launcher.
func programStatusFixture(t *testing.T, launcher string) (root, bin, hcLog string, cfg *config.Config) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("herdr-core is POSIX only")
	}
	root, _, bin = setupBoard(t)
	hcLog = writeFakeHC(t, bin, root)
	cfg = envConfig("plaincli", launcher, nil)
	cfg.ChatAgent = "plaincli"
	cfg.Agents = map[string]config.AgentDefinition{"plaincli": plainAgent(bin)}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	return root, bin, hcLog, cfg
}

func requireRefusal(t *testing.T, err error, hcLog string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "program_status") || !strings.Contains(err.Error(), "plaincli") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(hcLog); !os.IsNotExist(statErr) {
		t.Fatalf("hc ran before the refusal: %s", mustRead(t, hcLog))
	}
}

// noTaskFile fails the test when the entrance writes a task file.
func noTaskFile(t *testing.T) {
	t.Helper()
	old := createTaskFile
	createTaskFile = func(string, string, process.TaskFileTrailer) (string, error) {
		t.Fatal("a task file was written before the refusal")
		return "", nil
	}
	t.Cleanup(func() { createTaskFile = old })
}

func TestHerdrCoreRefusesUndeclaredAgentOnStart(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "tmux")
	taskID, path := makeTodo(t, root, "pstatus-start")
	before, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error { return commandStart(root, "plaincli", "herdr-core", taskID) })
	requireRefusal(t, err, hcLog)
	after, err := board.ReadSnapshot(root, taskID)
	if err != nil || after.Entry.State != "todo" || after.Revision != before.Revision {
		t.Fatalf("card moved or changed: %+v %v", after.Entry, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("card left todo")
	}
}

// An agent that declares it does not report is refused with its own reason.
func TestHerdrCoreRefusesNonReportingAgentOnStart(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "tmux")
	taskID, _ := makeTodo(t, root, "pstatus-cursor")
	_, _, err := capture(t, func() error { return commandStart(root, "cursor", "herdr-core", taskID) })
	if err == nil || err.Error() != config.Text("program_status.agent_not_reporting", "herdr-core", "cursor") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(hcLog); !os.IsNotExist(statErr) {
		t.Fatal("hc ran before the refusal")
	}
}

// startUnderTmux runs a card with plaincli in tmux, so a later recovery
// entrance meets a working card whose agent has no declaration.
func startUnderTmux(t *testing.T, root, slug string) (string, board.Snapshot) {
	t.Helper()
	taskID, _ := makeTodo(t, root, slug)
	if _, _, err := capture(t, func() error { return commandStart(root, "plaincli", "tmux", taskID) }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return taskID, snapshot
}

func requireUnchanged(t *testing.T, root string, before board.Snapshot) {
	t.Helper()
	after, err := board.ReadSnapshot(root, before.Entry.TaskID)
	if err != nil || after.Revision != before.Revision || after.Text != before.Text {
		t.Fatalf("card changed by a refused launch: %v", err)
	}
}

func TestHerdrCoreRefusesUndeclaredAgentOnResume(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "tmux")
	taskID, before := startUnderTmux(t, root, "pstatus-resume")
	noTaskFile(t)
	_, _, err := capture(t, func() error { return commandResume(root, nil, "herdr-core", taskID, "go on", "", true, 61) })
	requireRefusal(t, err, hcLog)
	requireUnchanged(t, root, before)
}

func TestHerdrCoreRefusesUndeclaredAgentOnTakeover(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "tmux")
	taskID, before := startUnderTmux(t, root, "pstatus-takeover")
	noTaskFile(t)
	agent := "plaincli"
	_, _, err := capture(t, func() error { return commandResume(root, &agent, "herdr-core", taskID, "take over", "", true, 61) })
	requireRefusal(t, err, hcLog)
	requireUnchanged(t, root, before)
}

func TestHerdrCoreRefusesUndeclaredAgentOnNotifyRecovery(t *testing.T) {
	root, _, hcLog, cfg := programStatusFixture(t, "tmux")
	_, before := startUnderTmux(t, root, "pstatus-notify")
	cfg.Launcher = "herdr-core"
	noTaskFile(t)
	_, err := NotifyViaResume(root, before.Entry, before.Text, "go on", 61)
	requireRefusal(t, err, hcLog)
	requireUnchanged(t, root, before)
}

func TestHerdrCoreRefusesUndeclaredAgentOnOrchestrate(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	taskID, path := makeTodo(t, root, "pstatus-orchestrate")
	noTaskFile(t)
	_, err := StartOrchestrator(OrchestrateRequest{Root: root, References: []string{taskID}, Handover: "plan", Agent: "plaincli", Launcher: "herdr-core"})
	requireRefusal(t, err, hcLog)
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("card left todo")
	}
}

func TestHerdrCoreRefusesUndeclaredAgentOnTriage(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	request := triageRequest(t, root)
	request.Agent, request.Launcher = "plaincli", "herdr-core"
	noTaskFile(t)
	_, err := StartTriage(request)
	requireRefusal(t, err, hcLog)
}

func TestHerdrCoreRefusesUndeclaredAgentOnRefinement(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	repo := triageRepository()
	now := time.Now().UTC()
	provider := &triageProvider{repository: repo, snapshot: issue.IssueSnapshot{Repository: repo, Number: 42, Title: "t", Body: "b", CommentsLoaded: true, CreatedAt: now, UpdatedAt: now, FetchedAt: now}}
	imported, err := issue.Import(t.Context(), provider, root, repo, 42, issue.ImportOptions{Language: "en", Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(imported.Path, "spec.md"))
	noTaskFile(t)
	_, err = issue.StartRefinement(t.Context(), root, repo, 42, issue.TriageOptions{CardID: imported.TaskID, Agent: "plaincli", Launcher: "herdr-core"})
	requireRefusal(t, err, hcLog)
	if after := mustRead(t, filepath.Join(imported.Path, "spec.md")); after != before {
		t.Fatal("refinement changed the card")
	}
}

func TestHerdrCoreRefusesUndeclaredAgentOnIssueResult(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	request := triageRequest(t, root)
	data, err := os.ReadFile(request.JSONPath)
	if err != nil {
		t.Fatal(err)
	}
	id := "20260914-pstatus-result-task"
	dir := filepath.Join(root, "done", id)
	if err := os.MkdirAll(filepath.Join(dir, "source"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, issue.SourceFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# Completed\n\n- SIZE: small\n\n## SUMMARY\n\nResolved.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request.CardID, request.ResultSync = id, true
	request.Agent, request.Launcher = "plaincli", "herdr-core"
	noTaskFile(t)
	_, err = StartTriage(request)
	requireRefusal(t, err, hcLog)
}

func TestHerdrCoreRefusesUndeclaredAgentOnChat(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	noTaskFile(t)
	_, err := StartChat(ChatRequest{Root: root, Message: "hello"})
	requireRefusal(t, err, hcLog)
}

// writeStatefulHC installs an hc that opens terminal t8, answers get with the
// file named by the get argument of the test, and records every call.
func writeStatefulHC(t *testing.T, bin, root string) (log, getFile string) {
	t.Helper()
	log = filepath.Join(root, "hc.log")
	getFile = filepath.Join(root, "hc.get")
	script := `#!/bin/sh
printf '%s\n' "$*" >> ` + process.AgentPath(log) + `
case "$1" in
run) printf '%s\n' '{"address":"t8","agent":"sh","state":"live","turn":0}' ;;
read) printf '%s\n' '{"complete":true,"how":"screen","text":"$ "}' ;;
get)
  if [ -f ` + process.AgentPath(getFile) + ` ]; then cat ` + process.AgentPath(getFile) + `; else
  printf '%s\n' '{"address":"t8","labels":{},"reported":false,"state":"live","status":null}'; fi ;;
ls) printf '%s\n' '{"hidden_finished":0,"terminals":[{"address":"t8","state":"live"}]}' ;;
kill|rm) printf '%s\n' '{"error":{"kind":"not_found","message":"t8 was removed"}}' >&2; exit 3 ;;
*) printf '{}\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return log, getFile
}

func reportApp(t *testing.T, getFile, app string) {
	t.Helper()
	body := `{"address":"t8","labels":{},"reported":true,"state":"live","status":{"app":"` + app + `","state":"idle"}}`
	if err := os.WriteFile(getFile, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A herdr-core start opens a parentless terminal that inherits the Kander
// environment, runs the agent by its resolved absolute path, records WINDOW
// as herdr-core:<terminal>, and stores the session in the terminal label.
func TestHerdrCoreStartRecordsTerminal(t *testing.T) {
	root, bin, _, _ := programStatusFixture(t, "herdr-core")
	log, _ := writeStatefulHC(t, bin, root)
	t.Setenv("KANDER_CONFIG", filepath.Join(root, "custom-config.json"))
	t.Setenv("KANBAN_DIR", root)
	taskID, _ := makeTodo(t, root, "pstatus-launch")
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "herdr-core", taskID) }); err != nil {
		t.Fatalf("start: %v\n%s", err, mustRead(t, log))
	}
	snapshot, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Entry.State != "working" || board.MetadataFrom(snapshot.Text, board.FieldWindow) != "herdr-core:t8" {
		t.Fatalf("state=%s text=%s", snapshot.Entry.State, snapshot.Text)
	}
	calls := strings.Split(strings.TrimSpace(mustRead(t, log)), "\n")
	run := calls[0]
	for _, want := range []string{"run --bg --no-parent --rm --json --cwd " + filepath.Dir(root) + " --name ", " --label kander-window=kb-",
		" -- /usr/bin/env PATH=", " KANBAN_DIR=" + root, " KANDER_CONFIG=" + filepath.Join(root, "custom-config.json"), " /bin/sh"} {
		if !strings.Contains(run, want) {
			t.Fatalf("run call %q lacks %q", run, want)
		}
	}
	claude, err := filepath.Abs(filepath.Join(bin, "claude"))
	if err != nil {
		t.Fatal(err)
	}
	var sent, labeled bool
	for _, call := range calls {
		sent = sent || strings.HasPrefix(call, "send --json t8 cd -- ") && strings.Contains(call, "&& exec "+claude+" ")
		labeled = labeled || strings.HasPrefix(call, "label --json t8 kander-session=")
	}
	if !sent || !labeled {
		t.Fatalf("agent command or session label missing:\n%s", strings.Join(calls, "\n"))
	}
}

func paneCoreAgent(bin string) config.AgentDefinition {
	def := paneAgentDef(filepath.Join(bin, "claude"))
	def.PromptDelivery.Ready.TimeoutMS = 400
	def.ProgramStatus = &config.ProgramStatus{Reports: true, App: "panecore-app"}
	return def
}

// Pane delivery waits for the first OSC 7501 report of the declared app and
// never reads the screen for readiness; an unreported terminal or another app
// fails the start the way a pane that never became ready does.
func TestHerdrCorePaneDeliveryWaitsForProgramStatus(t *testing.T) {
	for _, tc := range []struct {
		name, app string
		wantErr   string
	}{
		{"reported", "panecore-app", ""},
		{"other app", "codex", "program_status.app_mismatch"},
		{"never reported", "", "program_status.ready_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, bin, _, cfg := programStatusFixture(t, "herdr-core")
			cfg.Agents["panecore"] = paneCoreAgent(bin)
			log, getFile := writeStatefulHC(t, bin, root)
			if tc.app != "" {
				reportApp(t, getFile, tc.app)
			}
			taskID, _ := makeTodo(t, root, "pstatus-pane")
			_, _, err := capture(t, func() error { return commandStart(root, "panecore", "herdr-core", taskID) })
			calls := mustRead(t, log)
			prompted := strings.Contains(calls, "send --json t8 ") && strings.Contains(calls, "UTF-8")
			if tc.wantErr == "" {
				if err != nil || !prompted {
					t.Fatalf("err=%v calls=\n%s", err, calls)
				}
				return
			}
			var want string
			if tc.wantErr == "program_status.app_mismatch" {
				want = config.Text(tc.wantErr, "t8", tc.app, "panecore", "panecore-app")
			} else {
				want = config.Text(tc.wantErr, "t8", "panecore")
			}
			if err == nil || !strings.Contains(err.Error(), want) || prompted {
				t.Fatalf("err=%v prompted=%v", err, prompted)
			}
			if !strings.Contains(calls, "kill --json t8") {
				t.Fatalf("terminal not closed after the failed delivery:\n%s", calls)
			}
			snapshot, readErr := board.ReadSnapshot(root, taskID)
			if readErr != nil || snapshot.Entry.State != "todo" {
				t.Fatalf("card not rolled back: %+v %v", snapshot.Entry, readErr)
			}
		})
	}
}

// The TUI moves a backlog card to todo after its start preview; the preview
// refuses first, so the card stays in backlog.
func TestHerdrCorePreviewStartRefusesBeforeBacklogMove(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "herdr-core")
	path, err := board.NewTask(root, "chore", "pstatus-preview", "任务 pstatus-preview", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	taskID := filepath.Base(path)
	_, err = PreviewStart(root, taskID)
	requireRefusal(t, err, hcLog)
	snapshot, err := board.ReadSnapshot(root, taskID)
	if err != nil || snapshot.Entry.State != "backlog" {
		t.Fatalf("card left backlog: %+v %v", snapshot.Entry, err)
	}
}

// A durable dispatch resume refuses before it prepares the dispatch.
func TestHerdrCoreRefusesUndeclaredAgentBeforeDispatch(t *testing.T) {
	root, _, hcLog, _ := programStatusFixture(t, "tmux")
	taskID, before := startUnderTmux(t, root, "pstatus-dispatch")
	noTaskFile(t)
	_, _, err := capture(t, func() error {
		return commandResume(root, nil, "herdr-core", taskID, "sync", "", true, 61, DispatchOptions{Kind: "sync"})
	})
	requireRefusal(t, err, hcLog)
	requireUnchanged(t, root, before)
	if _, statErr := os.Stat(filepath.Join(before.Entry.Path, "dispatches")); !os.IsNotExist(statErr) {
		t.Fatalf("dispatch evidence written before the refusal: %v", statErr)
	}
}

// Notify recovery checks the configured launcher before it begins a dispatch
// attempt; another launcher passes.
func TestRecoveryPreflight(t *testing.T) {
	root, _, hcLog, cfg := programStatusFixture(t, "tmux")
	_, before := startUnderTmux(t, root, "pstatus-recovery")
	if err := RecoveryPreflight(before.Text); err != nil {
		t.Fatalf("tmux recovery refused: %v", err)
	}
	cfg.Launcher = "herdr-core"
	requireRefusal(t, RecoveryPreflight(before.Text), hcLog)
}

// A same-ID retry of an accepted dispatch keeps returning its receipt after the
// launcher changed to one that cannot run the card's agent: nothing launches.
func TestHerdrCoreKeepsDispatchReceiptRetry(t *testing.T) {
	root, task, d := resumeDispatchFixture(t)
	writeFakeHC(t, filepath.Join(root, "fake-bin"), root)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	cfg := envConfig("claude", "herdr-core", nil)
	cfg.Agents = map[string]config.AgentDefinition{"claude": {ProgramStatus: &config.ProgramStatus{Reports: false}}}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	out, _, err := capture(t, func() error {
		return commandResume(root, nil, "", task, d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
	})
	if err != nil || !strings.Contains(out, `"state":"accepted"`) {
		t.Fatalf("receipt retry: %s %v", out, err)
	}
	// A new dispatch for the same card is refused before it is prepared.
	before, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "", task, "again", "", true, 61, DispatchOptions{Kind: "sync", Base: strings.Repeat("a", 40)})
	})
	if err == nil || err.Error() != config.Text("program_status.agent_not_reporting", "herdr-core", "claude") {
		t.Fatalf("new dispatch: %v", err)
	}
	requireUnchanged(t, root, before)
}
