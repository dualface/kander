package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/rules"
)

// jsonLine parses text as exactly one JSON object and a newline.
func jsonLine(t *testing.T, text string) map[string]any {
	t.Helper()
	var object map[string]any
	if !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 1 || json.Unmarshal([]byte(text), &object) != nil {
		t.Fatalf("not one JSON object and a newline: %q", text)
	}
	return object
}

// requireResult checks a successful --json run: exit status 0, an empty stderr, one JSON object
// on stdout that holds every named field.
func requireResult(t *testing.T, r commandResult, what string, fields ...string) map[string]any {
	t.Helper()
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("%s: exit=%d\nstdout=%s\nstderr=%s", what, r.code, r.stdout, r.stderr)
	}
	object := jsonLine(t, r.stdout)
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			t.Fatalf("%s: result has no %q: %s", what, field, r.stdout)
		}
	}
	return object
}

// requireFailure checks a failed --json run: the exit status, an empty stdout, one error object
// on stderr with the given code. It returns the object of the error.
func requireFailure(t *testing.T, r commandResult, what, code string, status int) map[string]any {
	t.Helper()
	if r.code != status || r.stdout != "" {
		t.Fatalf("%s: exit=%d (want %d)\nstdout=%s\nstderr=%s", what, r.code, status, r.stdout, r.stderr)
	}
	object := jsonLine(t, r.stderr)
	detail, _ := object["error"].(map[string]any)
	if detail["code"] != code || detail["message"] == nil || detail["message"] == "" {
		t.Fatalf("%s: error object %s, want code %q", what, r.stderr, code)
	}
	return detail
}

func TestJSONIsRefusedWhereItHasNoMeaning(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"--json"}, {"worker", "--json"}} {
		r := runKander(t, args...)
		requireFailure(t, r, strings.Join(args, " "), "usage", 2)
	}
}

// list --mobile no longer exists: it is an unknown flag with or without --json, wherever the
// global flag stands.
func TestJSONListMobileIsAnUnknownFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	for _, args := range [][]string{{"list", "--mobile", "--json"}, {"--json", "list", "--mobile"}} {
		r := runKander(t, args...)
		detail := requireFailure(t, r, strings.Join(args, " "), "usage", 2)
		if message, _ := detail["message"].(string); !strings.Contains(message, "--mobile") {
			t.Fatalf("%v: message %q does not name the unknown flag", args, message)
		}
	}
	text := runKander(t, "list", "--mobile")
	if text.code != 2 || text.stdout != "" || !strings.Contains(text.stderr, "--mobile") {
		t.Fatalf("list --mobile: exit=%d\nstdout=%s\nstderr=%s", text.code, text.stdout, text.stderr)
	}
}

// Every command refuses an argument error with the error object when --json is given, wherever
// the flag stands, and without a project binding or a server.
func TestJSONUsageErrorOfEveryLeafCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		path := strings.Fields(leaf.path)
		for name, args := range map[string][]string{
			"json first": append(slices.Clone(path), "--json", "--no-such-flag"),
			"json last":  append(slices.Clone(path), "--no-such-flag", "--json"),
		} {
			r := runKander(t, args...)
			requireFailure(t, r, "kander "+leaf.path+" ("+name+")", "usage", 2)
		}
	}
}

func TestJSONMessageFollowsLanguageAndCodeStays(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	english := runCLI(t, "--lang", "en", "show", "--json", "20261118-sample-task")
	chinese := runCLI(t, "--lang", "cn", "show", "--json", "20261118-sample-task")
	en := requireFailure(t, english, "show unbound", remote.CodeNotConfigured, 1)
	zh := requireFailure(t, chinese, "show unbound cn", remote.CodeNotConfigured, 1)
	if en["message"] != "" && en["message"] == zh["message"] || zh["message"] != config.Text("remote.not_configured") {
		t.Fatalf("messages: en=%v cn=%v", en["message"], zh["message"])
	}
	// The exit status is the one of the text form.
	if text := runKander(t, "show", "20261118-sample-task"); text.code != english.code {
		t.Fatalf("text exit=%d, json exit=%d", text.code, english.code)
	}
}

func TestJSONFailuresHaveTheirStableCodes(t *testing.T) {
	session, cfg := moveServiceFixture(t)
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "failure-codes", Title: "Failure codes", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}

	// The service answers a document that does not exist with its API code, and the exit status of
	// the text form is the same.
	r := runKander(t, "show", "--json", "--document", "missing.md", task.TaskID)
	detail := requireFailure(t, r, "missing document", api.CodeNotFound, 1)
	if text := runKander(t, "show", "--document", "missing.md", task.TaskID); text.code != r.code || !strings.Contains(text.stderr, detail["message"].(string)) {
		t.Fatalf("text form: %+v", text)
	}
	// The service answers with an API error that carries no board code.
	r = runKander(t, "request", "answer", "--json", task.TaskID, "request-1", "done")
	requireFailure(t, r, "answer without a claim", api.CodeHeldByOther, 1)
	// A stale revision is a board rejection: the code is the board code.
	file := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(file, []byte(task.Text), 0o600); err != nil {
		t.Fatal(err)
	}
	r = runKander(t, "update", "--json", task.TaskID, "--document", "spec.md", "--file", file, "--expect-revision", strconv.Itoa(int(task.Revision)+7))
	requireFailure(t, r, "stale revision", "board.transaction_conflict", 1)

	// The service does not answer.
	cfg.Server.URL = "http://127.0.0.1:1"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	r = runKander(t, "show", "--json", task.TaskID)
	requireFailure(t, r, "unreachable service", remote.CodeUnreachable, 1)
	if text := runKander(t, "show", task.TaskID); text.code != r.code {
		t.Fatalf("text exit=%d", text.code)
	}
}

// The commands that read or write a card, with and without a claim, print their result object.
func TestJSONBoardCommands(t *testing.T) {
	session, _ := moveServiceFixture(t)
	t.Setenv(config.EnvLang, "en")

	version := requireResult(t, runKander(t, "version", "--json"), "version", "version")
	if version["version"] == "" {
		t.Fatal("empty version")
	}
	configuration := requireResult(t, runKander(t, "config", "--json"), "config", "agent_language", "rules", "launcher")
	if configuration["agent_language"] != "en" {
		t.Fatalf("config: %v", configuration)
	}
	bound := requireResult(t, runKander(t, "init", "--json"), "init", "project_name", "project_id", "server_url")
	if bound["project_name"] != "command-fixture" {
		t.Fatalf("init: %v", bound)
	}

	created := requireResult(t, runKander(t, "new", "--json", "chore", "json-output", "JSON", "output"), "new", "task_id", "state", "location", "revision")
	id := created["task_id"].(string)
	if created["state"] != "backlog" || created["location"] != "kander-server:command-fixture/"+id {
		t.Fatalf("new: %v", created)
	}
	listed := requireResult(t, runKander(t, "list", "--json"), "list", "schema_version", "change_token", "tasks")
	if tasks, _ := listed["tasks"].([]any); len(tasks) != 1 {
		t.Fatalf("list: %v", listed)
	}
	requireResult(t, runKander(t, "list", "--json", "--agent-status", "backlog"), "list --agent-status", "schema_version", "tasks")
	if r := runKander(t, "list", "--agent-status"); r.code != 2 || !strings.Contains(r.stderr, "--json") {
		t.Fatalf("--agent-status without --json: %+v", r)
	}

	shown := requireResult(t, runKander(t, "show", "--json", id), "show", "task_id", "state", "revision", "text")
	document := requireResult(t, runKander(t, "show", "--json", "--document", "spec.md", id), "show --document", "path", "text", "sha256", "expected_revision")
	if document["text"] != shown["text"] || document["path"] != "spec.md" {
		t.Fatalf("show --document: %v", document)
	}

	// update takes a file and the revision, and prints the new one.
	task, err := session.Project.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "spec.md")
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.ReplaceAll(text, "- [ ]", "- [x]")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := requireResult(t, runKander(t, "update", "--json", id, "--document", "spec.md", "--file", file, "--expect-revision", strconv.FormatUint(task.Revision, 10)), "update", "task_id", "document", "revision")
	if updated["task_id"] != id || updated["document"] != "spec.md" || updated["revision"].(float64) <= float64(task.Revision) {
		t.Fatalf("update: %v", updated)
	}

	moved := requireResult(t, runKander(t, "move", "--json", id, "todo"), "move todo", "task_id", "state", "location", "revision", "replayed")
	if moved["state"] != "todo" || moved["replayed"] != false || moved["completion_report"] != nil {
		t.Fatalf("move: %v", moved)
	}
	claimed := requireResult(t, runKander(t, "move", "--json", id, "working", "--owner", "codex"), "move --owner", "task_id", "state", "generation", "execution_id")
	if claimed["state"] != "working" {
		t.Fatalf("claim: %v", claimed)
	}
	// guard-write allows a write that is not on a card, and reports the absolute path.
	guard := requireResult(t, runKander(t, "guard-write", "--json", "notes.txt"), "guard-write", "path", "allowed")
	if guard["allowed"] != true || !filepath.IsAbs(guard["path"].(string)) {
		t.Fatalf("guard-write: %v", guard)
	}

	// Collaboration requests: a proposal, the list, the request itself and the holder's answer.
	sent := requireResult(t, runKander(t, "request", "--json", id, "sync", "please", "sync"), "request", "id", "status", "target_generation")
	requestID := sent["id"].(string)
	requests := requireResult(t, runKander(t, "request", "list", "--json", id), "request list", "requests")
	if got, _ := requests["requests"].([]any); len(got) != 1 {
		t.Fatalf("request list: %v", requests)
	}
	for _, args := range [][]string{{"request", "show", id, requestID}, {"request", "show", "--json", id, requestID}} {
		shownRequest := requireResult(t, runKander(t, args...), strings.Join(args, " "), "id", "kind", "message", "status")
		if shownRequest["id"] != requestID {
			t.Fatalf("request show: %v", shownRequest)
		}
	}
	answered := requireResult(t, runKander(t, "request", "answer", "--json", id, requestID, "accepted", "noted"), "request answer", "id", "status")
	if answered["status"] != api.RequestAccepted {
		t.Fatalf("request answer: %v", answered)
	}

	// Without --json a move into done keeps the location on stdout and the report on stderr; with it the
	// report is a field and stderr is empty.
	closeNonGitFixturePlan(t, session, task)
	finished := requireResult(t, runKander(t, "move", "--json", id, "done", "--result", "completed"), "move done", "task_id", "state", "completion_report")
	if finished["state"] != "done" || !strings.Contains(finished["completion_report"].(string), "# Kanban Task Completion Report") {
		t.Fatalf("done: %v", finished)
	}

	// pick: a backlog card named on the command line; the selection prompt is refused.
	other := requireResult(t, runKander(t, "new", "--json", "chore", "pick-output", "Pick"), "new", "task_id")["task_id"].(string)
	var pickedCard api.Task
	if pickedCard, err = session.Project.Show(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	filled := strings.ReplaceAll(pickedCard.Text, "<FILL_IN>", "filled")
	filled = strings.ReplaceAll(filled, "- [ ]", "- [x]")
	filled = strings.Replace(filled, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := session.Project.PutDocument(t.Context(), "", other, api.Document{Path: "spec.md", Text: filled, ExpectedRevision: pickedCard.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	picked := requireResult(t, runKander(t, "pick", "--json", other), "pick", "task_id", "state", "location", "revision")
	if picked["state"] != "todo" {
		t.Fatalf("pick: %v", picked)
	}
}

// A bound move prints the same object with and without --json, plus the report field with it.
func TestJSONBoundMoveAndDispatch(t *testing.T) {
	session, _ := moveServiceFixture(t)
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "bound-json", Title: "Bound JSON", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	task = readyServiceCard(t, session, task)
	credential, _, err := session.Credential(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	input := board.DispatchInput{ID: "json-round", TaskID: task.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Synchronize"}
	dispatch, err := session.Project.DispatchAction(t.Context(), "prepare-json", task.TaskID, "prepare", api.DispatchCommand{Input: &input, CardRevision: task.Revision}, *credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	epoch := strconv.FormatUint(dispatch.Authorization.Epoch, 10)
	// The prompt that Kander generates for the executing agent names the field it has to read.
	prompt, err := rules.File("templates/prompt/dispatch.md")
	if err != nil || !strings.Contains(string(prompt), "replayed=false") {
		t.Fatalf("the dispatch prompt does not name the replayed field: %v", err)
	}
	for _, replayed := range []bool{false, true} {
		got := requireResult(t, runKander(t, "move", "--json", task.TaskID, "working", "--dispatch-id", dispatch.Input.ID, "--execution-epoch", epoch), "bound move", "dispatch", "replayed")
		if got["replayed"] != replayed {
			t.Fatalf("bound move: %v", got)
		}
	}
	shown := requireResult(t, runKander(t, "dispatch", "show", "--json", task.TaskID, dispatch.Input.ID), "dispatch show", "intent", "state")
	if shown["state"] != string(board.DispatchAccepted) {
		t.Fatalf("dispatch show: %v", shown)
	}
	// The output is the same without --json.
	text := runKander(t, "dispatch", "show", task.TaskID, dispatch.Input.ID).require(t, "dispatch show")
	if jsonLine(t, text.stdout)["state"] != string(board.DispatchAccepted) {
		t.Fatalf("text form: %s", text.stdout)
	}
}

// A card goes through start, check, notify, resume and dismiss with --json. The terminal is the
// fake multiplexer of the definition tests.
func TestJSONLaunchLifecycle(t *testing.T) {
	fixture := fakeMuxFixture(t)
	service, state := fixture.service, fixture.state

	id := readyCard(t, "json-lifecycle")
	started := requireResult(t, runKander(t, "start", "--json", "--launcher", "fakemux", id), "start",
		"action", "task_id", "size", "agent", "launcher", "address", "replayed", "superseded")
	if started["action"] != "start" || started["task_id"] != id || started["launcher"] != "fakemux" || started["address"] != "fakemux:w1:p1" || started["replayed"] != false {
		t.Fatalf("start: %v", started)
	}

	waitCommandFact(t, func() bool {
		task, err := service.Project.Show(t.Context(), id)
		return err == nil && task.Claim.Observation != nil && task.Claim.Observation.Status == "alive"
	})
	checked := requireResult(t, runKander(t, "check", "--json", id), "check", "exit_code", "output", "liveness")
	liveness, _ := checked["liveness"].([]any)
	if len(liveness) != 1 || liveness[0].(map[string]any)["status"] != "alive" || liveness[0].(map[string]any)["task_id"] != id {
		t.Fatalf("check liveness: %v", checked)
	}

	notified := requireResult(t, runKander(t, "notify", "--json", "--message", "status please", id), "notify", "id", "task_id", "status", "target_generation")
	if notified["status"] != "open" {
		t.Fatalf("notify: %v", notified)
	}
	waitCommandFact(t, func() bool {
		output, _ := os.ReadFile(filepath.Join(state, "panes", "p1", "output"))
		return strings.Contains(string(output), "kander request show "+id)
	})

	// A takeover needs a confirmed stop of the first container and an explicit decision.
	if err := os.RemoveAll(filepath.Join(state, "panes", "p1")); err != nil {
		t.Fatal(err)
	}
	resumed := requireResult(t, runKander(t, "resume", "--json", "--agent", "claude", "--timeout", "61", "--message", "take over", "--decision", "user restarts the stopped executor", id), "resume",
		"action", "task_id", "agent", "launcher", "address")
	if resumed["action"] != "resume" || resumed["address"] != "fakemux:w2:p2" {
		t.Fatalf("resume: %v", resumed)
	}

	// dismiss closes the container of a card that is archived.
	dismissed := readyCard(t, "json-dismiss")
	requireResult(t, runKander(t, "start", "--json", "--launcher", "fakemux", dismissed), "start for dismiss", "task_id")
	runKander(t, "move", dismissed, "archived", "--result", "cancelled", "--reason", "fixture", "--decision", "test").require(t, "archive")
	result := requireResult(t, runKander(t, "dismiss", "--json", "--timeout", "61", dismissed), "dismiss", "task_id", "outcome")
	if result["outcome"] != "dismissed" && result["outcome"] != "target_gone" {
		t.Fatalf("dismiss: %v", result)
	}

	// orchestrate hands cards in todo to one orchestrator session.
	planned := readyCard(t, "json-orchestrate")
	orchestrated := requireResult(t, runKander(t, "orchestrate", "--json", "--launcher", "fakemux", "--message", "go", planned), "orchestrate", "agent", "launcher", "address", "tasks")
	if tasks, _ := orchestrated["tasks"].([]any); len(tasks) != 1 || tasks[0] != planned || orchestrated["launcher"] != "fakemux" {
		t.Fatalf("orchestrate: %v", orchestrated)
	}
}

// A form that needs a terminal is refused before it has any effect.
func TestJSONInteractiveFormsAreRefusedWithoutEffects(t *testing.T) {
	fixture := fakeMuxFixture(t)
	id := readyCard(t, "json-interactive")
	configPath := os.Getenv(config.EnvConfig)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	forms := map[string][]string{
		"install":                  {"install", "--json"},
		"uninstall without --yes":  {"uninstall", "--json"},
		"pick without a task":      {"pick", "--json"},
		"start without a task":     {"start", "--json"},
		"start with foreground":    {"start", "--json", "--launcher", "foreground", id},
		"start with console":       {"start", "--json", "--launcher", "console", id},
		"orchestrate with console": {"orchestrate", "--json", "--launcher", "console", "--message", "go", id},
		"resume with foreground":   {"resume", "--json", "--agent", "claude", "--decision", "test", "--message", "go", "--launcher", "foreground", id},
		"issue triage foreground":  {"issue", "triage", "--json", "--launcher", "foreground", "42"},
		"issue result foreground":  {"issue", "result", "--json", "--launcher", "foreground", "--card", id, "42"},
	}
	// The refusal comes before the client state of the first run: with an empty client home
	// nothing is created in it.
	clientHome := t.TempDir()
	t.Setenv(remote.EnvClientHome, clientHome)
	// A host that has been used before: its identity exists, and the project of the card has no
	// local directory yet (and the service no registration of it here). The refusals still create
	// nothing besides the identity.
	if _, err := remote.ClientID(clientHome); err != nil {
		t.Fatal(err)
	}
	for name, args := range forms {
		r := runKander(t, args...)
		requireFailure(t, r, name, "interactive_required", 2)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the configuration changed: %v", err)
	}
	if entries, _ := os.ReadDir(clientHome); len(entries) != 1 || entries[0].Name() != "client-id" {
		t.Fatalf("the refusals created client state besides the identity: %v", entries)
	}
	if got := cardField(t, id, board.FieldOwner); got != "" {
		t.Fatalf("the card was claimed: owner=%q", got)
	}
	if task, err := fixture.service.Project.Show(t.Context(), id); err != nil || task.State != "todo" {
		t.Fatalf("the card moved: %+v %v", task, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(fixture.state, "panes")); len(entries) != 0 {
		t.Fatalf("a container was created: %v", entries)
	}
	// No receipt was written either, so the same request can be sent again without --json.
	root, err := fixture.service.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "launch-actions")); len(entries) != 0 {
		t.Fatalf("a launch receipt was written before the refusal: %v", entries)
	}
	// uninstall with --yes needs no answer.
	uninstalled := requireResult(t, runKander(t, "uninstall", "--json", "--yes"), "uninstall --yes", "mode", "config_file", "rules_dir", "items")
	if uninstalled["mode"] != "global" {
		t.Fatalf("uninstall: %v", uninstalled)
	}
}

// doctor never asks a question with --json: it reports its checks as lines.
func TestJSONDoctor(t *testing.T) {
	fakeMuxFixture(t)
	r := runKander(t, "doctor", "--json")
	if r.code == 2 {
		// This machine has something an interactive doctor would ask (an unusable agent to
		// replace, a modified rule file): the run is refused, with nothing repaired.
		requireFailure(t, r, "doctor needs a confirmation", "interactive_required", 2)
		return
	}
	if r.code != 0 && r.code != 1 || r.stderr != "" {
		t.Fatalf("doctor: exit=%d stderr=%s", r.code, r.stderr)
	}
	object := jsonLine(t, r.stdout)
	lines, _ := object["lines"].([]any)
	if _, ok := object["healthy"].(bool); !ok || len(lines) == 0 || (r.code == 0) != object["healthy"].(bool) {
		t.Fatalf("doctor: exit=%d %v", r.code, object)
	}
	first := lines[0].(map[string]any)
	if first["level"] == nil || first["message"] == nil {
		t.Fatalf("doctor line: %v", first)
	}
}

func TestJSONTerminalCommands(t *testing.T) {
	fakeMuxFixture(t)
	listed := runKander(t, "terminal", "list", "--json")
	object := requireResult(t, listed, "terminal list", "definitions")
	found := false
	for _, entry := range object["definitions"].([]any) {
		if entry.(map[string]any)["name"] == "fakemux" {
			found = true
			for _, field := range []string{"status", "source", "path", "detail", "active", "invalid"} {
				if _, ok := entry.(map[string]any)[field]; !ok {
					t.Fatalf("definition has no %q: %v", field, entry)
				}
			}
		}
	}
	if !found {
		t.Fatalf("fakemux is not listed: %s", listed.stdout)
	}
	requireFailure(t, runKander(t, "terminal", "test", "--json", "nosuchlauncher"), "terminal test unknown", "failed", 1)
	// A conformance run is a result: its steps and whether they passed.
	tested := runKander(t, "terminal", "test", "--json", "--skip-focus", "fakemux")
	if tested.stderr != "" || (tested.code != 0 && tested.code != 1) {
		t.Fatalf("terminal test: %+v", tested)
	}
	result := jsonLine(t, tested.stdout)
	steps, _ := result["steps"].([]any)
	if result["launcher"] != "fakemux" || len(steps) == 0 || (tested.code == 0) != (result["passed"] == true) {
		t.Fatalf("terminal test: exit=%d %v", tested.code, result)
	}
	if step := steps[0].(map[string]any); step["name"] == nil || step["status"] == nil {
		t.Fatalf("step: %v", step)
	}
}

func runServerJSON(t *testing.T, args ...string) commandResult {
	t.Helper()
	code, stdout, stderr := commandtest.Capture(func() int { return runServer(args) })
	return commandResult{code: code, stdout: stdout, stderr: stderr}
}

// The service lifecycle and the offline commands print their result object.
func TestJSONServerCommands(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	dir := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	})
	cfg := config.DefaultConfig()
	cfg.Server.URL = target
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	fields := []string{"url", "version", "pid", "data_dir", "log_path", "projects", "listen", "ttl", "renew"}

	requireFailure(t, runServerJSON(t, "status", "--json"), "status of a stopped server", "server_not_running", 1)
	started := requireResult(t, runServerJSON(t, "start", "--json", "--data", dir, "--listen", address), "start", fields...)
	if started["data_dir"] != dir || started["listen"] != address {
		t.Fatalf("start: %v", started)
	}
	status := requireResult(t, runServerJSON(t, "status", "--json"), "status", fields...)
	if status["pid"] != started["pid"] {
		t.Fatalf("status: %v", status)
	}
	restarted := requireResult(t, runServerJSON(t, "restart", "--json"), "restart", fields...)
	if restarted["pid"] == started["pid"] || restarted["data_dir"] != dir {
		t.Fatalf("restart: %v", restarted)
	}
	stopped := requireResult(t, runServerJSON(t, "stop", "--json"), "stop", "stopped", "target")
	if stopped["stopped"] != true || stopped["target"] != target {
		t.Fatalf("stop: %v", stopped)
	}
	if again := requireResult(t, runServerJSON(t, "stop", "--json"), "second stop", "stopped"); again["stopped"] != false {
		t.Fatalf("second stop: %v", again)
	}

	// The offline commands hold the data directory, so the server is stopped.
	listed := requireResult(t, runServerJSON(t, "projects", "--json", "--data", dir), "projects", "projects")
	if projects, ok := listed["projects"].([]any); !ok || len(projects) != 0 {
		t.Fatalf("projects: %v", listed)
	}
	source := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(source, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The fields the contract lists for the import result, with their types; the optional ones
	// (existing, defaulted_size, warnings) are left out of an empty first import.
	imported := requireResult(t, runServerJSON(t, "import", "--json", "--data", dir, "--project", "json-demo", "--from", source), "import", "project", "source", "files", "tasks", "snapshot")
	for _, optional := range []string{"existing", "defaulted_size", "warnings"} {
		if _, present := imported[optional]; present {
			t.Fatalf("import: the empty field %q is not left out: %v", optional, imported)
		}
	}
	if project, _ := imported["project"].(map[string]any); project["name"] != "json-demo" || project["id"] == "" || project["created_at"] == nil {
		t.Fatalf("import: project %v", imported["project"])
	}
	if _, ok := imported["files"].(float64); !ok {
		t.Fatalf("import: files %v", imported["files"])
	}
	listed = requireResult(t, runServerJSON(t, "projects", "--json", "--data", dir), "projects after import", "projects")
	if projects, _ := listed["projects"].([]any); len(projects) != 1 || projects[0].(map[string]any)["name"] != "json-demo" {
		t.Fatalf("projects after import: %v", listed)
	}
	maintained := requireResult(t, runServerJSON(t, "maintain", "--json", "--data", dir, "--project", "json-demo", "--maintenance"), "maintain", "project", "changed_tasks", "snapshot")
	if _, ok := maintained["changed_tasks"].(float64); !ok || maintained["snapshot"] == "" {
		t.Fatalf("maintain: %v", maintained)
	}
	// A failure writes only the error object, with no log line before it.
	requireFailure(t, runServerJSON(t, "maintain", "--json", "--data", dir, "--project", "no-such-project"), "maintain unknown", api.CodeNotFound, 1)
}

// kander server start --foreground --json writes one object when the server can serve. A failure
// after that keeps it and adds the error object; a failure before it leaves stdout empty.
func TestJSONServerForeground(t *testing.T) {
	serverCommandEnv(t)
	original := runForegroundServer
	defer func() { runForegroundServer = original }()
	dir := t.TempDir()

	runForegroundServer = func(_ context.Context, o server.Options, _ string) error {
		o.Ready("127.0.0.1:4242")
		return nil
	}
	r := runServerJSON(t, "start", "--json", "--foreground", "--data", dir)
	ready := requireResult(t, r, "start --foreground", "listen", "data_dir", "pid", "version")
	if ready["listen"] != "127.0.0.1:4242" || ready["data_dir"] != dir || ready["pid"] != float64(os.Getpid()) {
		t.Fatalf("ready: %v", ready)
	}

	runForegroundServer = func(_ context.Context, o server.Options, _ string) error {
		o.Ready("127.0.0.1:4242")
		return errors.New("serving stopped")
	}
	r = runServerJSON(t, "start", "--json", "--foreground", "--data", dir)
	if r.code != 1 || jsonLine(t, r.stdout)["listen"] != "127.0.0.1:4242" {
		t.Fatalf("failure after ready: %+v", r)
	}
	detail := jsonLine(t, r.stderr)["error"].(map[string]any)
	if detail["code"] != "failed" || !strings.Contains(detail["message"].(string), "serving stopped") {
		t.Fatalf("error object: %s", r.stderr)
	}

	runForegroundServer = func(context.Context, server.Options, string) error { return errors.New("cannot listen") }
	requireFailure(t, runServerJSON(t, "start", "--json", "--foreground", "--data", dir), "failure before ready", "failed", 1)
	requireFailure(t, runServerJSON(t, "start", "--json", "--foreground", "--ttl", "1s", "--renew", "2s"), "invalid lease", "usage", 2)
}

// jsonStreamLines parses every line of text as one JSON object.
func jsonStreamLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		var object map[string]any
		if json.Unmarshal([]byte(line), &object) != nil {
			t.Fatalf("line is not a JSON object: %q", line)
		}
		lines = append(lines, object)
	}
	return lines
}

// subscribe --json: a failure before the first line leaves stdout empty and writes one error
// object; a failure after output started keeps the lines and ends stderr with the error object,
// with the exit status of the text form.
func TestJSONSubscribeFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	// Before any output: the checkout is not bound.
	r := runKander(t, "subscribe", "--json", "--watch", "20261118-sample-task")
	requireFailure(t, r, "subscribe unbound", remote.CodeNotConfigured, 1)

	// After output started: the service goes away while the subscription runs.
	cfg := config.DefaultConfig()
	cfg.Language, cfg.AgentLanguage = "en", "en"
	session, httpServer := commandServiceWithServer(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "subscribe-json", Title: "Subscribe JSON", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		// Long enough for the first snapshot on a loaded machine.
		time.Sleep(4 * time.Second)
		httpServer.CloseClientConnections()
		httpServer.Close()
	}()
	result := runKander(t, "subscribe", "--json", "--refresh", "1", "--watch", task.TaskID)
	<-finished
	if result.code != 1 {
		t.Fatalf("exit=%d\nstdout=%s\nstderr=%s", result.code, result.stdout, result.stderr)
	}
	events := jsonStreamLines(t, result.stdout)
	if len(events) < 2 {
		t.Fatalf("stdout=%q", result.stdout)
	}
	if terminal := events[len(events)-1]["event"]; events[0]["event"] != "snapshot" || terminal != "read-error" && terminal != "membership-unknown" {
		t.Fatalf("events=%v", events)
	}
	errorLines := jsonStreamLines(t, result.stderr)
	last := errorLines[len(errorLines)-1]["error"].(map[string]any)
	if last["code"] == nil || last["message"] == "" {
		t.Fatalf("stderr=%s", result.stderr)
	}
	for _, line := range errorLines[:len(errorLines)-1] {
		if line["notice"] == nil {
			t.Fatalf("a diagnostic line is neither a notice nor the error: %v", line)
		}
	}
}

// The Git analyzers print their completed result as one object, and nothing on stderr; the
// exit status is the one of the analysis.
func TestJSONCheckAnalyzers(t *testing.T) {
	repo := t.TempDir()
	gitIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn("init", "-q", "-b", "main")
	gitIn("commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn("add", "a.txt")
	gitIn("commit", "-q", "-m", "change")
	t.Chdir(repo)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(repo))

	delivery := requireResult(t, runKander(t, "check", "delivery", "--json", "--base", "HEAD~1"), "check delivery", "schema_version", "check", "status", "diff_check", "added_over_limit")
	if delivery["check"] != "delivery" || delivery["status"] != "pass" {
		t.Fatalf("delivery: %v", delivery)
	}
	overlap := requireResult(t, runKander(t, "check", "overlap", "--json", "--source", "HEAD~1"), "check overlap", "schema_version", "check", "status", "paths")
	if overlap["check"] != "overlap" || overlap["status"] != "pass" {
		t.Fatalf("overlap: %v", overlap)
	}
	// Whitespace damage is a finding: the result with exit status 1, not a failure.
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("trailing \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn("add", "b.txt")
	gitIn("commit", "-q", "-m", "whitespace")
	finding := runKander(t, "check", "delivery", "--json", "--base", "HEAD~1")
	if finding.code != 1 || finding.stderr != "" || jsonLine(t, finding.stdout)["status"] != "fail" {
		t.Fatalf("finding: %+v", finding)
	}
	// An analysis that could not finish is the error object.
	requireFailure(t, runKander(t, "check", "delivery", "--json", "--base", "no-such-ref"), "unknown ref", "check_invalid_ref", 3)
}

// A foreground server whose configured port is taken fails after the data directory opened and
// logged. With --json the log lines before the failure are remarks of the one error object:
// stdout is empty and stderr is one line.
func TestJSONServerForegroundPortConflictIsOneErrorObject(t *testing.T) {
	serverCommandEnv(t)
	original := runForegroundServer
	runForegroundServer = server.RunForeground
	defer func() { runForegroundServer = original }()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	r := runServerJSON(t, "start", "--json", "--foreground", "--data", t.TempDir(), "--listen", taken.Addr().String())
	if r.code != 1 || r.stdout != "" || strings.Count(r.stderr, "\n") != 1 {
		t.Fatalf("exit=%d\nstdout=%q\nstderr=%q", r.code, r.stdout, r.stderr)
	}
	var object struct {
		Error   map[string]any   `json:"error"`
		Notices []map[string]any `json:"notices"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &object); err != nil || object.Error["code"] == nil {
		t.Fatalf("stderr=%q %v", r.stderr, err)
	}
	if len(object.Notices) == 0 || object.Notices[0]["code"] != "server_log" {
		t.Fatalf("the log lines before the failure are not remarks of the error object: %s", r.stderr)
	}
}

// A subscription that fails before its first snapshot is one error object on stderr and nothing
// else; the remarks made before the first event (TestStreamingRemarksWaitForTheFirstEvent in
// internal/command) belong to that object.
func TestJSONSubscribeFailureBeforeTheFirstSnapshotIsOneErrorObject(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	cfg := config.DefaultConfig()
	cfg.Language, cfg.AgentLanguage = "en", "en"
	_, httpServer := commandServiceWithServer(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	httpServer.Close()
	r := runKander(t, "subscribe", "--json", "--watch", "20261118-sample-task")
	if r.code != 1 || r.stdout != "" || strings.Count(r.stderr, "\n") != 1 {
		t.Fatalf("exit=%d\nstdout=%q\nstderr=%q", r.code, r.stdout, r.stderr)
	}
	if object := jsonLine(t, r.stderr); object["error"] == nil || object["notice"] != nil {
		t.Fatalf("stderr=%q", r.stderr)
	}
}

// A refusal comes before the session of the host is opened, which registers the project with the
// service. The host has real receipts in project A: one start and one orchestrator session that
// succeeded (it has no takeover receipt). The same client then works in project B, which the
// service does not know. The receipts of A say nothing about B, so start, an explicit takeover
// and orchestrate are refused for their launcher before B is registered.
func TestJSONInteractiveRefusalDoesNotRegisterTheProject(t *testing.T) {
	fixture := fakeMuxFixture(t)
	started := readyCard(t, "json-receipt-start")
	planned := readyCard(t, "json-receipt-orchestrate")
	requireResult(t, runKander(t, "start", "--json", "--launcher", "fakemux", started), "start in project A", "task_id")
	requireResult(t, runKander(t, "orchestrate", "--json", "--launcher", "fakemux", "--message", "go", planned), "orchestrate in project A", "tasks")
	root, err := fixture.service.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "launch-actions")); len(entries) < 2 {
		t.Fatalf("project A holds no receipts: %v", entries)
	}
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Project.Name = "unregistered-project-b"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// The task IDs of the receipts of project A are reused, but the refused calls use a launcher
	// that takes the terminal (foreground, console) where A used fakemux, so their receipt keys
	// differ from those of A as well.
	for name, args := range map[string][]string{
		"start":       {"start", "--json", "--launcher", "foreground", started},
		"takeover":    {"resume", "--json", "--agent", "claude", "--decision", "test", "--message", "go", "--launcher", "foreground", started},
		"orchestrate": {"orchestrate", "--json", "--launcher", "console", "--message", "go", planned},
	} {
		requireFailure(t, runKander(t, args...), name, "interactive_required", 2)
	}
	if project, err := fixture.service.Client.ProjectByName(t.Context(), "unregistered-project-b"); err == nil {
		t.Fatalf("the refusals registered the project: %+v", project)
	}
}

// lookupProxy stands in front of the service. It answers the first failures read-only lookups of
// one project name with not_ready, as a service that is restarting does, forwards everything else,
// and records the names of the projects that a POST asks it to register.
type lookupProxy struct {
	mu       sync.Mutex
	name     string
	failures int
	lookups  int
	posts    []string
}

// arm makes the next failures lookups fail and starts counting lookups again.
func (p *lookupProxy) arm(failures int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures, p.lookups = failures, 0
}

func (p *lookupProxy) state() (lookups int, posts []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lookups, slices.Clone(p.posts)
}

func newLookupProxy(t *testing.T, target, name string) (*lookupProxy, string) {
	t.Helper()
	upstream, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &lookupProxy{name: name}
	forward := httputil.NewSingleHostReverseProxy(upstream)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			proxy.mu.Lock()
			fail := false
			switch r.Method {
			case http.MethodGet:
				if r.URL.Query().Get("name") == proxy.name {
					proxy.lookups++
					fail = proxy.failures > 0
					if fail {
						proxy.failures--
					}
				}
			case http.MethodPost:
				var register api.RegisterProject
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &register)
				r.Body = io.NopCloser(bytes.NewReader(body))
				proxy.posts = append(proxy.posts, register.Name)
			}
			proxy.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotReady, Message: "restarting"})
				return
			}
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return proxy, server.URL
}

// A refusal that cannot read the project does not go on to register it. The host has a client
// identity, and the configuration now names project B, which the service does not know. The
// service is restarting: the read-only lookup before the session fails with not_ready on every
// attempt the client makes. The call ends with that error, and the service never sees a POST that
// would create B or bind its Git source. When the service is back the same call is refused for its
// launcher and B is still unregistered.
func TestJSONInteractiveRefusalDoesNotRegisterWhileTheLookupFails(t *testing.T) {
	fixture := fakeMuxFixture(t)
	started := readyCard(t, "json-lookup-start")
	planned := readyCard(t, "json-lookup-orchestrate")
	const missing = "unregistered-project-b"
	proxy, proxyURL := newLookupProxy(t, fixture.cfg.Server.URL, missing)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Project.Name, cfg.Server.URL = missing, proxyURL
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// One call of the client makes Retries+1 attempts; failing exactly that many exhausts it.
	probe, err := client.New(proxyURL, "attempt-probe")
	if err != nil {
		t.Fatal(err)
	}
	attempts := probe.Retries + 1
	home, err := remote.Home()
	if err != nil {
		t.Fatal(err)
	}
	namespaces := func() []string {
		entries, _ := os.ReadDir(filepath.Join(home, "projects"))
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	before := namespaces()
	for _, refused := range []struct {
		name string
		args []string
	}{
		{"start", []string{"start", "--json", "--launcher", "foreground", started}},
		{"takeover", []string{"resume", "--json", "--agent", "claude", "--decision", "test", "--message", "go", "--launcher", "foreground", started}},
		{"orchestrate", []string{"orchestrate", "--json", "--launcher", "console", "--message", "go", planned}},
	} {
		name, args := refused.name, refused.args
		proxy.arm(attempts)
		failed := runKander(t, args...)
		lookups, posts := proxy.state()
		if len(posts) != 0 {
			t.Fatalf("%s registered projects while the lookup failed: %v\nexit=%d stderr=%s", name, posts, failed.code, failed.stderr)
		}
		if lookups < attempts {
			t.Fatalf("%s: %d lookups, want at least %d: the failing path was not taken", name, lookups, attempts)
		}
		requireFailure(t, failed, name+" while the lookup fails", "not_ready", 1)
		if after := namespaces(); !slices.Equal(after, before) {
			t.Fatalf("%s created a namespace for project B: before=%v after=%v", name, before, after)
		}
		proxy.arm(0)
		requireFailure(t, runKander(t, args...), name+" once the service answers", "interactive_required", 2)
		if _, posts := proxy.state(); len(posts) != 0 {
			t.Fatalf("%s registered projects after the lookup recovered: %v", name, posts)
		}
	}
	if project, err := fixture.service.Client.ProjectByName(t.Context(), missing); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("project B must stay unregistered: %+v, %v", project, err)
	}
}
