package launch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

func devinDiscovery(t *testing.T) *config.SessionDiscovery {
	t.Helper()
	disc := config.AgentFor(config.DefaultConfig(), "devin").Session.Discovery
	if disc == nil {
		t.Fatal("devin embedded definition declares no session discovery")
	}
	return disc
}

func opencodeDiscovery(t *testing.T) *config.SessionDiscovery {
	t.Helper()
	disc := config.AgentFor(config.DefaultConfig(), "opencode").Session.Discovery
	if disc == nil {
		t.Fatal("opencode embedded definition declares no session discovery")
	}
	return disc
}

func kimiDiscovery(t *testing.T) *config.SessionDiscovery {
	t.Helper()
	disc := config.AgentFor(config.DefaultConfig(), "kimi").Session.Discovery
	if disc == nil {
		t.Fatal("kimi embedded definition declares no session discovery")
	}
	return disc
}

func TestKimiSessionIsDiscoveredAfterStart(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("kimi", &process.AgentProgram{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "kimi" || session.Reference != "" {
		t.Fatalf("session=%+v", session)
	}
	def := config.AgentFor(cfg, "kimi").Session
	if def.Mode != "discovered" || !config.SessionDiscoversAfterStart(def) || !config.SessionPersistsAfterStart(def) {
		t.Fatalf("mode=%q", def.Mode)
	}
}

func TestParseSessionListKimiWorkDirMatch(t *testing.T) {
	cwd := t.TempDir()
	disc := kimiDiscovery(t)
	out := `[{"id":"k_other","workDir":"/elsewhere","lastPrompt":"task","updatedAt":1},` +
		`{"id":"k_mine","workDir":"` + cwd + `","lastPrompt":"执行任务 task; full instructions are in the UTF-8 task file","updatedAt":2},` +
		`{"id":"k_missing","lastPrompt":"no directory","updatedAt":3}]`
	got, err := parseSessionList(disc, out, cwd, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"k_mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestOpenCodeSessionIsDiscoveredAfterStart(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("opencode", &process.AgentProgram{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "opencode" || session.Reference != "" {
		t.Fatalf("session=%+v", session)
	}
	def := config.AgentFor(cfg, "opencode").Session
	if def.Mode != "discovered" || !config.SessionDiscoversAfterStart(def) || !config.SessionPersistsAfterStart(def) {
		t.Fatalf("mode=%q", def.Mode)
	}
}

func TestParseSessionListOpenCodeDirectoryMatch(t *testing.T) {
	cwd := t.TempDir()
	disc := opencodeDiscovery(t)
	out := `[{"id":"ses_other","directory":"/elsewhere","title":"x"},` +
		`{"id":"ses_mine","directory":"` + cwd + `","title":"reply"},` +
		`{"id":"ses_missing","title":"no directory"}]`
	got, err := parseSessionList(disc, out, cwd, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ses_mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestDevinSessionIsDiscoveredAfterStart(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("devin", &process.AgentProgram{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "devin" || session.Reference != "" {
		t.Fatalf("session=%+v", session)
	}
	def := config.AgentFor(cfg, "devin").Session
	if def.Mode != "discovered" || !config.SessionDiscoversAfterStart(def) || !config.SessionPersistsAfterStart(def) {
		t.Fatalf("mode=%q", def.Mode)
	}
}

func TestParseSessionList(t *testing.T) {
	cwd := t.TempDir()
	disc := devinDiscovery(t)
	got, err := parseSessionList(disc, `[{"id":"session-2","working_directory":"`+cwd+`"},{"id":"session-1","working_directory":"`+cwd+`"},{"id":"session-2","working_directory":"`+cwd+`"}]`, cwd, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"session-2", "session-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
	for _, data := range []string{
		`not-json`, `null`, `[{"title":"missing","working_directory":"` + cwd + `"}]`,
		`[{"id":"bad id","working_directory":"` + cwd + `"}]`, `[{"id":42,"working_directory":"` + cwd + `"}]`,
	} {
		if _, err := parseSessionList(disc, data, cwd, "task"); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestParseSessionListMatchesProjectDirectory(t *testing.T) {
	cwd := t.TempDir()
	disc := devinDiscovery(t)
	out := `[{"id":"other-project","working_directory":"/elsewhere"},` +
		`{"id":"mine","working_directory":"` + cwd + `"},` +
		`{"id":"missing-dir"}]`
	got, err := parseSessionList(disc, out, cwd, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestParseSessionListMatchesSymlinkedDirectory(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink unavailable")
	}
	disc := devinDiscovery(t)
	got, err := parseSessionList(disc, `[{"id":"mine","working_directory":"`+real+`"}]`, link, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestParseSessionListFormats(t *testing.T) {
	cwd := t.TempDir()
	jsonl := &config.SessionDiscovery{Format: "jsonl", IDField: "sid"}
	got, err := parseSessionList(jsonl, "{\"sid\":\"a\"}\n{\"sid\":\"b\"}\n\n{\"sid\":\"a\"}", cwd, "task")
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("jsonl got=%q err=%v", got, err)
	}
	if _, err := parseSessionList(jsonl, "{\"sid\":\"a\"}\nbroken", cwd, "task"); err == nil {
		t.Fatal("accepted malformed jsonl")
	}
	lines := &config.SessionDiscovery{Format: "lines"}
	got, err = parseSessionList(lines, "s1\n\ns2\ns1\n", cwd, "task")
	if err != nil || !reflect.DeepEqual(got, []string{"s1", "s2"}) {
		t.Fatalf("lines got=%q err=%v", got, err)
	}
	if _, err := parseSessionList(lines, "ok\nbad id\n", cwd, "task"); err == nil {
		t.Fatal("accepted invalid line id")
	}
}

func TestParseSessionListTaskIDMatch(t *testing.T) {
	cwd := t.TempDir()
	disc := &config.SessionDiscovery{
		Format:  "json",
		IDField: "id",
		Match:   []config.DiscoveryMatch{{Field: "title", Equals: "task-{task_id}"}},
	}
	got, err := parseSessionList(disc, `[{"id":"a","title":"task-1"},{"id":"b","title":"task-2"}]`, cwd, "2")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestDiscoverNewAgentSessionUsesSetDifference(t *testing.T) {
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		return []string{"existing", "new-session"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	got, err := discoverNewAgentSession(devinDiscovery(t), "task", map[string]struct{}{"existing": {}}, &process.AgentProgram{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-session" {
		t.Fatalf("session=%q", got)
	}
}

func TestDiscoverNewAgentSessionRejectsMultipleCandidates(t *testing.T) {
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		return []string{"one", "two"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	disc := devinDiscovery(t)
	_, err := discoverNewAgentSession(disc, "task", map[string]struct{}{}, &process.AgentProgram{}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscoverNewAgentSessionTimesOutWithoutCandidates(t *testing.T) {
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		return []string{"existing"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	previousNow := nowFn
	now := previousNow()
	nowFn = func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
	t.Cleanup(func() { nowFn = previousNow })
	disc := devinDiscovery(t)
	if _, err := discoverNewAgentSession(disc, "task", map[string]struct{}{"existing": {}}, &process.AgentProgram{}, t.TempDir()); err == nil {
		t.Fatal("discovery without a new session must fail")
	}
}

func TestDiscoverNewAgentSessionStopsEnumeratingPastDeadline(t *testing.T) {
	calls := 0
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		calls++
		return []string{"existing"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	previousNow := nowFn
	now := previousNow()
	nowFn = func() time.Time { return now }
	t.Cleanup(func() { nowFn = previousNow })
	previousSleep := sleepFn
	sleepFn = func(time.Duration) { now = now.Add(time.Hour) }
	t.Cleanup(func() { sleepFn = previousSleep })
	disc := devinDiscovery(t)
	if _, err := discoverNewAgentSession(disc, "task", map[string]struct{}{"existing": {}}, &process.AgentProgram{}, t.TempDir()); err == nil {
		t.Fatal("discovery without a new session must fail")
	}
	if calls != 1 {
		t.Fatalf("enumerate ran %d times past the deadline", calls)
	}
}

func fakeEnumerate(t *testing.T, script string) *process.AgentProgram {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fake")
	}
	path := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &process.AgentProgram{Path: path}
}

func TestEnumerateAgentSessionsCommandFailure(t *testing.T) {
	program := fakeEnumerate(t, "echo broken >&2\nexit 3\n")
	disc := devinDiscovery(t)
	ctx, cancel := probe.TimeoutContext(5 * time.Second)
	defer cancel()
	if _, err := enumerateAgentSessions(ctx, disc, program, t.TempDir(), "task"); err == nil {
		t.Fatal("nonzero enumerate must fail")
	}
}

func TestEnumerateAgentSessionsRunsInProjectDirectory(t *testing.T) {
	cwd := t.TempDir()
	program := fakeEnumerate(t, `printf '[{"id":"mine","working_directory":"%s"}]' "$(pwd)"`+"\n")
	disc := devinDiscovery(t)
	ctx, cancel := probe.TimeoutContext(5 * time.Second)
	defer cancel()
	got, err := enumerateAgentSessions(ctx, disc, program, cwd, "task")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestEnumerateAgentSessionsRejectsOversizedOutput(t *testing.T) {
	program := fakeEnumerate(t, "yes | head -c 8192 | tr -d '\\n'\n")
	disc := &config.SessionDiscovery{Args: []string{}, Format: "lines", MaxBytes: 4096}
	ctx, cancel := probe.TimeoutContext(5 * time.Second)
	defer cancel()
	if _, err := enumerateAgentSessions(ctx, disc, program, t.TempDir(), "task"); err == nil {
		t.Fatal("oversized output must fail")
	}
}

func TestEnumerateAgentSessionsHonorsDeadline(t *testing.T) {
	program := fakeEnumerate(t, "sleep 5\n")
	disc := devinDiscovery(t)
	ctx, cancel := probe.TimeoutContext(100 * time.Millisecond)
	defer cancel()
	if _, err := enumerateAgentSessions(ctx, disc, program, t.TempDir(), "task"); err == nil {
		t.Fatal("deadline must cancel a hanging enumerate")
	}
}

func TestAgentsWithoutDiscoveryNeverEnumerate(t *testing.T) {
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		t.Fatal("enumerate ran for a non-discovered agent")
		return nil, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	for _, agent := range []string{"codex", "claude", "cursor"} {
		def := config.AgentFor(config.DefaultConfig(), agent).Session
		if def.Mode == "discovered" {
			t.Fatalf("%s must not declare discovered", agent)
		}
		if _, err := sessionDiscoverSnapshot(def, "task", true, &process.AgentProgram{}, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartDiscoveryEnumerationFailureKeepsCardInTodo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		return nil, launchError("launch.session_list_failed", "stub")
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	taskID, path := makeTodo(t, root, "disc-snapshot-fail")
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) }); err == nil {
		t.Fatal("start must fail when the baseline enumeration fails")
	}
	if _, stat := os.Stat(path); stat != nil {
		t.Fatal("card must stay in todo")
	}
}

func TestStartPostLaunchDiscoveryFailureRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	previousList := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		return []string{"existing"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previousList })
	previousNow := nowFn
	now := previousNow()
	nowFn = func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
	t.Cleanup(func() { nowFn = previousNow })
	taskID, path := makeTodo(t, root, "disc-post-fail")
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) }); err == nil {
		t.Fatal("start must fail when no new session appears")
	}
	if _, stat := os.Stat(path); stat != nil {
		t.Fatal("card must roll back to todo")
	}
}
