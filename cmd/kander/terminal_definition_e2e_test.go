package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/dualface/kander/internal/install/installtest"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/focus"
	"github.com/dualface/kander/internal/terminal"
)

// fakeMuxScript is a third-party terminal multiplexer emulated in sh. Panes
// live as directories under $FAKEMUX_STATE; typing /exit ends the agent.
const fakeMuxScript = `#!/bin/sh
state="$FAKEMUX_STATE"
printf '%s\n' "$*" >> "$state/calls.log"
pane_dir() { printf '%s/panes/%s' "$state" "$1"; }
case "$1" in
  --version) printf 'fakemux 1.0\n' ;;
  new)
    count=$(cat "$state/count" 2>/dev/null || printf 0); count=$((count+1)); printf '%s' "$count" > "$state/count"
    dir=$(pane_dir "p$count"); mkdir -p "$dir"
    printf 'w%s' "$count" > "$dir/window"; printf 'sh' > "$dir/command"; printf '0' > "$dir/dead"; : > "$dir/output"
    printf 'w%s\tp%s\n' "$count" "$count" ;;
  run)
    dir=$(pane_dir "$2"); [ -d "$dir" ] || { printf 'no such pane: %s\n' "$2" >&2; exit 1; }
    command=${3#exec }
    case "$command" in
      env\ *)
        command=${command#env }
        while :; do first=${command%% *}; case "$first" in *=*) command=${command#* } ;; *) break ;; esac; done ;;
    esac
    command=${command%% *}; command=${command#\'}; command=${command%\'}
    printf '%s' "${command##*/}" > "$dir/command"; printf '%s' "$3" > "$dir/launched" ;;
  meta-set) printf '%s' "$3" > "$(pane_dir "$2")/marker" ;;
  meta-get)
    dir=$(pane_dir "$2"); [ -d "$dir" ] || { printf 'no such pane: %s\n' "$2" >&2; exit 1; }
    [ -f "$dir/marker" ] || exit 4
    cat "$dir/marker" ;;
  facts)
    dir=$(pane_dir "$2"); [ -d "$dir" ] || { printf 'no such pane: %s\n' "$2" >&2; exit 1; }
    printf '%s\t0\t%s\n' "$(cat "$dir/command")" "$(cat "$dir/dead")" ;;
  read) cat "$(pane_dir "$2")/output" ;;
  type)
    dir=$(pane_dir "$2"); printf '%s' "$3" >> "$dir/output"
    [ "$3" = "/exit" ] && printf '1' > "$dir/dead"
    # Act as the agent: read the notify message file and output its marker.
    message=$(printf '%s' "$3" | grep -o '/[^ ,]*/message\.txt')
    [ -z "$message" ] || grep -o 'KANDER-NOTIFY-ACK:[0-9a-f]*' "$message" >> "$dir/output" ;;
  key) printf '\n' >> "$(pane_dir "$2")/output" ;;
  topology)
    dir=$(pane_dir "$2"); [ -d "$dir" ] || { printf 'no such pane: %s\n' "$2" >&2; exit 1; }
    printf '%s\t1\n' "$(cat "$dir/window")" ;;
  exists)
    for dir in "$state"/panes/*; do
      [ -d "$dir" ] && [ "$(cat "$dir/window")" = "$2" ] && exit 0
    done
    printf 'no such window: %s\n' "$2" >&2; exit 1 ;;
  list)
    for dir in "$state"/panes/*; do
      [ -d "$dir" ] || continue
      printf '%s\t%s\t%s\t%s\t%s\n' "${dir##*/}" "$(cat "$dir/window")" "$(cat "$dir/command")" "$(cat "$dir/dead")" "$(cat "$dir/marker" 2>/dev/null)"
    done ;;
  focus) printf '%s %s\n' "$2" "$3" >> "$state/focus.log" ;;
  close)
    for dir in "$state"/panes/*; do
      [ -d "$dir" ] && [ "$(cat "$dir/window")" = "$2" ] && rm -rf "$dir"
    done ;;
  *) printf 'unknown command %s\n' "$1" >&2; exit 2 ;;
esac
exit 0
`

const fakeMuxDefinition = `{
  "schema_version": 1,
  "name": "fakemux",
  "binary": "fakemuxctl",
  "version_args": ["--version"],
  "capabilities": {"container": true, "focus": true, "pane_metadata": true, "foreground_process": true},
  "address": [{"name": "container"}, {"name": "pane"}],
  "errors": {"gone": ["stderr:^no such (?:pane|window)"], "meta_missing": ["exit:4"]},
  "launchers": {
    "fakemux": {
      "requires": {"platform": "posix", "binary": true, "inside_session": false},
      "started_lines": [{"message": "{head}\tlauncher=fakemux\twindow={container}"}]
    }
  },
  "ops": {
    "create_container": {
      "steps": [{
        "store": "new",
        "argv": ["new", "{cwd}", "{label}"],
        "output": {"source": "stdout", "parse": "regex:^(\\S+\\t\\S+)"},
        "fields": {"container": "regex:^(\\S+)\\t", "pane": "regex:\\t(\\S+)$"}
      }],
      "result": {"container": "{step.new.container}", "pane": "{step.new.pane}"}
    },
    "run_command": {"steps": [{"argv": ["run", "{pane}", "{command}"]}]},
    "set_session_marker": {"steps": [{"argv": ["meta-set", "{pane}", "{value}"]}]},
    "pane_facts": {
      "steps": [
        {
          "store": "facts",
          "argv": ["facts", "{pane}"],
          "fields": {"command": "regex:^([^\\t]+)\\t", "mode": "regex:^[^\\t]+\\t([01])\\t", "dead": "regex:\\t([01])\\n?$"},
          "expect": "!field_missing:step.facts.dead"
        },
        {"store": "marker", "argv": ["meta-get", "{pane}"], "on_error": "meta_missing"}
      ],
      "result": {"command": "{step.facts.command}", "in_mode": "{step.facts.mode}", "dead": "{step.facts.dead}", "session_marker": "{step.marker.text}"}
    },
    "read_output": {"steps": [{"store": "out", "argv": ["read", "{pane}"]}], "result": {"text": "{step.out.text}"}},
    "deliver_text": {"steps": [{"argv": ["type", "{pane}", "{text}"]}, {"argv": ["key", "{pane}", "Enter"]}]},
    "topology": {
      "steps": [{"store": "topo", "argv": ["topology", "{pane}"], "fields": {"window": "regex:^(\\S+)\\t", "count": "regex:\\t(\\d+)"}}],
      "result": {"container": "{step.topo.window}", "pane_count": "{step.topo.count}"}
    },
    "container_exists": {"steps": [{"argv": ["exists", "{container}"]}]},
    "reverse_lookup": {
      "steps": [{"store": "list", "argv": ["list"]}],
      "rows": {
        "from": "list",
        "fields": {"pane": "regex:^(\\S+)\\t", "window": "regex:^\\S+\\t(\\S+)\\t", "command": "regex:^(?:[^\\t]*\\t){2}([^\\t]+)\\t", "dead": "regex:^(?:[^\\t]*\\t){3}([01])\\t", "marker": "regex:\\t([^\\t]*)$"},
        "expect": [["!field_missing:row.pane", "!field_missing:row.window", "!field_missing:row.dead"]],
        "match": [["!field_missing:reference", "field:row.marker={reference}", "field:row.dead=0", "field:row.command={process_name}"]],
        "result": {"container": "{row.window}", "pane": "{row.pane}"}
      }
    },
    "focus": {"closed_when": "field:facts.dead=1", "steps": [{"argv": ["focus", "{container}", "{pane}"]}]},
    "close_container": {"steps": [{"when": "!field_missing:container", "argv": ["close", "{container}"]}]}
  }
}`

type commandResult struct {
	code   int
	stdout string
	stderr string
}

// runKander runs one command of the full binary with captured output.
func runKander(t *testing.T, args ...string) commandResult {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&stdout, outR); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&stderr, errR); done <- struct{}{} }()
	os.Stdout, os.Stderr = outW, errW
	code := cli.Run(append([]string{"kander", "--lang", "en"}, args...))
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()
	<-done
	<-done
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (r commandResult) require(t *testing.T, what string) commandResult {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("%s exit=%d\nstdout=%s\nstderr=%s", what, r.code, r.stdout, r.stderr)
	}
	return r
}

func cardField(t *testing.T, id, field string) string {
	t.Helper()
	snapshot, err := currentCommandSession(t).Project.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return board.MetadataFrom(snapshot.Text, field)
}

// A third-party definition placed in the share directory runs the complete
// lifecycle through the same Backend paths as the built-in launchers.
func TestTerminalDefinitionLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake terminal is a POSIX shell script")
	}
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	home := t.TempDir()
	t.Setenv("HOME", home)
	installtest.IsolateAgentConfigEnv(t)
	shareDir := filepath.Join(home, ".local", "share", "kander", terminal.DefinitionsDirName)
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definitionPath := filepath.Join(shareDir, "fakemux.json")
	if err := os.WriteFile(definitionPath, []byte(fakeMuxDefinition), 0o644); err != nil {
		t.Fatal(err)
	}
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)

	bin := t.TempDir()
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, "panes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "fakemuxctl"), []byte(fakeMuxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEMUX_STATE", state)
	for _, name := range []string{"TMUX", "TMUX_PANE", "HERDR_ENV"} {
		t.Setenv(name, "")
	}

	root := t.TempDir()
	for _, name := range board.States {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	t.Setenv(config.EnvConfig, filepath.Join(home, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language, cfg.AgentLanguage = "en", "en"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	cfg.Launcher = "fakemux"
	service := commandService(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	// config --json accepts and prints the definition launcher; doctor lists
	// the definition as loaded.
	var printed struct {
		Launcher string `json:"launcher"`
	}
	configOut := runKander(t, "config", "--json").require(t, "config --json")
	if err := json.Unmarshal([]byte(configOut.stdout), &printed); err != nil || printed.Launcher != "fakemux" {
		t.Fatalf("config --json launcher=%q err=%v out=%s", printed.Launcher, err, configOut.stdout)
	}
	doctor := runKander(t, "doctor")
	if want := config.Text("terminal.definition_loaded", definitionPath, "fakemux"); !strings.Contains(doctor.stdout+doctor.stderr, want) {
		t.Fatalf("doctor does not list the definition:\n%s%s", doctor.stdout, doctor.stderr)
	}
	// doctor repairs the configuration around the fake agent; restore it.
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	id := readyCard(t, "definition-lifecycle")
	runKander(t, "start", "--launcher", "fakemux", id).require(t, "start")
	window := cardField(t, id, board.FieldWindow)
	if window != "fakemux:w1:p1" {
		t.Fatalf("WINDOW=%q", window)
	}
	if backend, address, ok := terminal.ParseWindow(window); !ok || backend.Name() != "fakemux" || address.Pane != "p1" {
		t.Fatalf("WINDOW is not parseable: %q", window)
	}
	session := strings.TrimPrefix(cardField(t, id, board.FieldSession), "claude ")
	if marker, _ := os.ReadFile(filepath.Join(state, "panes", "p1", "marker")); string(marker) != session {
		t.Fatalf("pane marker=%q session=%q", marker, session)
	}

	waitCommandFact(t, func() bool {
		task, err := service.Project.Show(t.Context(), id)
		return err == nil && task.Claim.Observation != nil && task.Claim.Observation.Status == "alive"
	})
	check := runKander(t, "check", id)
	if !strings.Contains(check.stdout, "status=alive") {
		t.Fatalf("check is not alive:\n%s%s", check.stdout, check.stderr)
	}

	notify := runKander(t, "notify", "--message", "status please", id).require(t, "notify proposal")
	if !strings.Contains(notify.stdout, `"status":"open"`) {
		t.Fatalf("notify did not return an open proposal: %s", notify.stdout)
	}
	waitCommandFact(t, func() bool {
		output, _ := os.ReadFile(filepath.Join(state, "panes", "p1", "output"))
		return strings.Contains(string(output), "kander request show "+id)
	})
	requests, err := service.Project.Requests(t.Context(), id)
	if err != nil || len(requests) != 1 || requests[0].Status != api.RequestAccepted {
		t.Fatalf("holder delivery receipt: %+v %v", requests, err)
	}
	// A fresh executor requires a confirmed local stop and an explicit decision.
	if err := os.RemoveAll(filepath.Join(state, "panes", "p1")); err != nil {
		t.Fatal(err)
	}

	resume := runKander(t, "resume", "--agent", "claude", "--timeout", "61", "--message", "take over", "--decision", "user explicitly restarts the stopped executor", id).require(t, "resume takeover")
	if got := cardField(t, id, board.FieldWindow); got != "fakemux:w2:p2" {
		t.Fatalf("takeover WINDOW=%q\n%s", got, resume.stdout)
	}
	if _, err := os.Stat(filepath.Join(state, "panes", "p1")); !os.IsNotExist(err) || !strings.Contains(resume.stdout, "window=w2") {
		t.Fatalf("original container kept after takeover:\n%s", resume.stdout)
	}

	if result := focus.Window(context.Background(), "fakemux:w2:p2"); !result.Success {
		t.Fatalf("focus=%+v", result)
	}
	if log, _ := os.ReadFile(filepath.Join(state, "focus.log")); string(log) != "w2 p2\n" {
		t.Fatalf("focus log=%q", log)
	}

	// Liveness turns stopped once the pane is gone and no pane matches.
	if err := os.RemoveAll(filepath.Join(state, "panes", "p2")); err != nil {
		t.Fatal(err)
	}
	waitCommandFact(t, func() bool {
		task, err := service.Project.Show(t.Context(), id)
		return err == nil && task.Claim.Observation != nil && task.Claim.Observation.Status == "stopped"
	})
	if check := runKander(t, "check", id); !strings.Contains(check.stdout, "status=stopped") {
		t.Fatalf("check after pane loss:\n%s%s", check.stdout, check.stderr)
	}

	// Dismiss exits the agent and closes a fresh container of an archived card.
	dismissID := readyCard(t, "definition-dismiss")
	runKander(t, "start", "--launcher", "fakemux", dismissID).require(t, "start for dismiss")
	runKander(t, "move", dismissID, "archived", "--result", "cancelled", "--reason", "fixture", "--decision", "test").require(t, "archive")
	runKander(t, "dismiss", "--timeout", "61", dismissID).require(t, "dismiss")
	if _, err := os.Stat(filepath.Join(state, "panes", "p3")); !os.IsNotExist(err) {
		t.Fatal("dismiss did not close the container")
	}
	if calls, _ := os.ReadFile(filepath.Join(state, "calls.log")); !strings.Contains(string(calls), "type p3 /exit") {
		t.Fatalf("dismiss did not deliver the exit command:\n%s", calls)
	}

	// A damaged definition is reported by doctor without affecting built-in launchers.
	if err := os.WriteFile(definitionPath, []byte(`{"schema_version": 9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	terminal.ReloadDefinitions()
	doctor = runKander(t, "doctor")
	if !strings.Contains(doctor.stdout+doctor.stderr, "unknown schema version 9") || !strings.Contains(doctor.stdout+doctor.stderr, definitionPath) {
		t.Fatalf("doctor does not report the damaged definition:\n%s%s", doctor.stdout, doctor.stderr)
	}
	if _, ok := terminal.Lookup("tmux"); !ok || config.ValidLauncherName("fakemux") {
		t.Fatal("a damaged definition must drop only its own launcher")
	}
	if result := focus.Window(context.Background(), "fakemux:w9:p9"); result.Success || result.Message != config.Text("focus.unsupported") {
		t.Fatalf("focus without a definition=%+v", result)
	}
}

func readyCard(t *testing.T, slug string) string {
	t.Helper()
	service := currentCommandSession(t)
	task, err := service.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Definition " + slug, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, board.Placeholder, "filled")
	discussion := "## " + board.SectionDiscussion + "\n"
	text = strings.Replace(text, discussion, discussion+"\n"+board.MarkerSelfReview+": pass\n"+board.MarkerCardReview+": pass\n", 1)
	if _, err := service.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	runKander(t, "move", task.TaskID, "todo").require(t, "move todo")
	return task.TaskID
}
