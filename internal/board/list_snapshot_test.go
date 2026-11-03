package board

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// listJSON runs list with the given observer and decodes the snapshot twice:
// once typed and once as raw maps, so key presence can be checked.
func listJSON(t *testing.T, observe AgentStatusFunc, args ...string) (ListSnapshot, map[string]any) {
	t.Helper()
	code, out, stderr := capture(t, func() int { return RunListWithAgentStatus(append([]string{"--json"}, args...), observe) })
	if code != 0 {
		t.Fatalf("list --json %v: code=%d stderr=%s", args, code, stderr)
	}
	var snapshot ListSnapshot
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	return snapshot, raw
}

func TestListJSONSnapshotContract(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	if code, _, stderr := capture(t, func() int { return RunNew([]string{"feature", "json-backlog", "待办卡"}) }); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := capture(t, func() int { return RunNew([]string{"--large", "bug", "json-working", "执行卡"}) }); code != 0 {
		t.Fatal(stderr)
	}
	workingID := todayID("json-working")
	spec := filepath.Join(root, "backlog", workingID, "spec.md")
	makeReady(t, spec)
	for _, move := range [][]string{{workingID, "todo"}, {workingID, "working", "--owner", "claude"}} {
		if code, _, stderr := capture(t, func() int { return RunMove(move) }); code != 0 {
			t.Fatal(stderr)
		}
	}
	setMeta(t, filepath.Join(root, "working", workingID, "spec.md"), "- WINDOW:\n", "- WINDOW: herdr:w1:t1:w1:p1\n")

	snapshot, raw := listJSON(t, nil)
	if snapshot.SchemaVersion != 1 || snapshot.Root != root || len(snapshot.ChangeToken) != 64 {
		t.Fatalf("top level %+v", snapshot)
	}
	if _, err := time.Parse(time.RFC3339, snapshot.GeneratedAt); err != nil {
		t.Fatalf("generated_at %q: %v", snapshot.GeneratedAt, err)
	}
	topTypes := map[string]string{"schema_version": "float64", "change_token": "string", "generated_at": "string", "root": "string", "tasks": "[]interface {}"}
	for key, want := range topTypes {
		if got := typeName(raw[key]); got != want {
			t.Fatalf("%s type %s, want %s", key, got, want)
		}
	}
	stringKeys := []string{"task_id", "title", "state", "size", "type", "task_group", "owner", "session", "window",
		"launcher", "created_at", "started_at", "finished_at", "result"}
	for _, item := range raw["tasks"].([]any) {
		task := item.(map[string]any)
		for _, key := range stringKeys {
			if typeName(task[key]) != "string" {
				t.Fatalf("%v: %s missing or not a string", task["task_id"], key)
			}
		}
		if typeName(task["revision"]) != "float64" {
			t.Fatalf("%v: revision %v", task["task_id"], task["revision"])
		}
		if value, ok := task["agent"]; !ok || value != nil {
			t.Fatalf("%v: agent %v present=%v", task["task_id"], value, ok)
		}
		if len(task) != len(stringKeys)+2 {
			t.Fatalf("%v: unexpected keys %v", task["task_id"], task)
		}
	}

	if len(snapshot.Tasks) != 2 {
		t.Fatalf("tasks %+v", snapshot.Tasks)
	}
	backlog, working := snapshot.Tasks[0], snapshot.Tasks[1]
	if working.TaskID != workingID || working.State != "working" || working.Size != "large" || working.Type != "Bug" {
		t.Fatalf("working %+v", working)
	}
	if working.Window != "herdr:w1:t1:w1:p1" || working.Launcher != "herdr" || working.StartedAt == "" || working.Owner != "claude" {
		t.Fatalf("working window %+v", working)
	}
	if working.Result != "" || working.FinishedAt != "" {
		t.Fatalf("empty fields %+v", working)
	}
	shown, err := ReadSnapshot(root, workingID)
	if err != nil {
		t.Fatal(err)
	}
	if working.Revision != shown.Revision {
		t.Fatalf("revision %d, show %d", working.Revision, shown.Revision)
	}
	if backlog.State != "backlog" || backlog.Title != "待办卡" || backlog.Window != "" || backlog.Launcher != "" || backlog.Owner != "" {
		t.Fatalf("backlog %+v", backlog)
	}

	filtered, _ := listJSON(t, nil, "working")
	if len(filtered.Tasks) != 1 || filtered.Tasks[0].TaskID != workingID {
		t.Fatalf("filtered %+v", filtered.Tasks)
	}
	if filtered.ChangeToken == snapshot.ChangeToken {
		t.Fatal("a filtered snapshot must digest only its own cards")
	}
}

func typeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "float64"
	case bool:
		return "bool"
	case []any:
		return "[]interface {}"
	case map[string]any:
		return "map"
	}
	return "other"
}

func TestListJSONChangeTokenFollowsBoardWrites(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	token := func() string {
		snapshot, _ := listJSON(t, nil)
		return snapshot.ChangeToken
	}
	empty := token()
	if again := token(); again != empty {
		t.Fatalf("unchanged board: %s then %s", empty, again)
	}

	if code, _, stderr := capture(t, func() int { return RunNew([]string{"chore", "token-card", "令牌卡"}) }); code != 0 {
		t.Fatal(stderr)
	}
	id := todayID("token-card")
	created := token()
	if created == empty {
		t.Fatal("new did not change the token")
	}
	if again := token(); again != created {
		t.Fatal("unchanged board after new changed the token")
	}

	shown, err := ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(input, []byte(strings.Replace(shown.Text, "## DISCUSSION\n", "## DISCUSSION\n\n补充讨论\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	update := []string{id, "--document", "spec.md", "--file", input, "--expect-revision", strconv.FormatUint(shown.Revision, 10)}
	if code, _, stderr := capture(t, func() int { return RunUpdate(update) }); code != 0 {
		t.Fatal(stderr)
	}
	updated := token()
	if updated == created {
		t.Fatal("update did not change the token")
	}

	makeReady(t, filepath.Join(root, "backlog", id, "spec.md"))
	edited := token()
	if edited == updated {
		t.Fatal("a body edit did not change the token")
	}
	if code, _, stderr := capture(t, func() int { return RunMove([]string{id, "todo"}) }); code != 0 {
		t.Fatal(stderr)
	}
	if moved := token(); moved == edited {
		t.Fatal("move did not change the token")
	}
}

func TestListJSONAgentStatusObservesOnlyInFlightWindows(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	for _, slug := range []string{"agent-working", "agent-review", "agent-nowindow", "agent-backlog"} {
		if code, _, stderr := capture(t, func() int { return RunNew([]string{"feature", slug, slug}) }); code != 0 {
			t.Fatal(stderr)
		}
	}
	for _, slug := range []string{"agent-working", "agent-review", "agent-nowindow"} {
		id := todayID(slug)
		makeReady(t, filepath.Join(root, "backlog", id, "spec.md"))
		for _, state := range []string{"todo", "working"} {
			if code, _, stderr := capture(t, func() int { return RunMove([]string{id, state}) }); code != 0 {
				t.Fatal(stderr)
			}
		}
	}
	setMeta(t, filepath.Join(root, "working", todayID("agent-working"), "spec.md"), "- WINDOW:\n", "- WINDOW: fake:p1\n")
	reviewSpec := filepath.Join(root, "working", todayID("agent-review"), "spec.md")
	setMeta(t, reviewSpec, "- WINDOW:\n", "- WINDOW: fake:p2\n")
	setMeta(t, reviewSpec, "- TASK_BRANCH:\n", "- TASK_BRANCH: agent-review\n")
	if code, _, stderr := capture(t, func() int { return RunMove([]string{todayID("agent-review"), "review"}) }); code != 0 {
		t.Fatal(stderr)
	}
	setMeta(t, filepath.Join(root, "backlog", todayID("agent-backlog"), "spec.md"), "- WINDOW:\n", "- WINDOW: fake:p3\n")

	var calls [][]AgentStatusRequest
	runtime := "working"
	observe := func(_ context.Context, requests []AgentStatusRequest) []ListAgent {
		calls = append(calls, requests)
		agents := make([]ListAgent, len(requests))
		for index := range requests {
			agents[index] = ListAgent{Status: "alive", RuntimeState: runtime, Reason: "fake", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
		}
		return agents
	}

	plain, _ := listJSON(t, observe)
	if len(calls) != 0 {
		t.Fatalf("list --json without --agent-status observed %v", calls)
	}
	for _, task := range plain.Tasks {
		if task.Agent != nil {
			t.Fatalf("%s agent without --agent-status", task.TaskID)
		}
	}

	first, _ := listJSON(t, observe, "--agent-status")
	if len(calls) != 1 || len(calls[0]) != 2 {
		t.Fatalf("observe calls %v", calls)
	}
	for _, task := range first.Tasks {
		inFlight := task.TaskID == todayID("agent-working") || task.TaskID == todayID("agent-review")
		if inFlight != (task.Agent != nil) {
			t.Fatalf("%s (%s) agent %+v", task.TaskID, task.State, task.Agent)
		}
	}
	second, _ := listJSON(t, observe, "--agent-status")
	if second.ChangeToken != first.ChangeToken {
		t.Fatal("observed_at alone changed the token")
	}
	runtime = "blocked"
	if third, _ := listJSON(t, observe, "--agent-status"); third.ChangeToken == second.ChangeToken {
		t.Fatal("an agent change did not change the token")
	}
}

func TestListJSONOptionErrors(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	observe := func(context.Context, []AgentStatusRequest) []ListAgent { return nil }
	for _, args := range [][]string{{"--json", "--mobile"}, {"--json", "nope"}} {
		if code, _, _ := capture(t, func() int { return RunListWithAgentStatus(args, observe) }); code != 2 {
			t.Fatalf("%v: code=%d", args, code)
		}
	}
	if code, _, stderr := capture(t, func() int { return RunListWithAgentStatus([]string{"--agent-status"}, observe) }); code != 2 || !strings.Contains(stderr, "--agent-status") {
		t.Fatalf("--agent-status without --json: code=%d %s", code, stderr)
	}
	if code, _, stderr := capture(t, func() int { return RunList([]string{"--json", "--agent-status"}) }); code != 2 || !strings.Contains(stderr, "--agent-status") {
		t.Fatalf("board-only list accepted --agent-status: code=%d %s", code, stderr)
	}
}
