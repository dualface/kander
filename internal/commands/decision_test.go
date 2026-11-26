package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/cli"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

func TestDecisionThroughCLI(t *testing.T) {
	runGit, run, p := boundCardCLI(t)
	runGit("config", "--local", "user.name", "CLI Recorder")
	card, err := p.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "cli-decision", Title: "Record a decision"})
	if err != nil {
		t.Fatal(err)
	}
	id := card.TaskID
	path := filepath.Join(t.TempDir(), "choice.json")
	if err := os.WriteFile(path, []byte(`{
		"question": "Which target?",
		"options": [{"key": "local", "label": "Local"}],
		"answer": "local",
		"channel": "pane",
		"authorization": {"granted_by":"Owner","wording":"Choose the test target","date":"2026-11-25"}
	}`), 0600); err != nil {
		t.Fatal(err)
	}
	raw := run("decision", "record", id, "--kind", "leader", "--file", path)
	var record api.DecisionRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.ID == "" || record.Kind != api.DecisionLeader ||
		record.RecordedBy != "CLI Recorder" || record.Authorization == nil || record.Authorization.GrantedBy != "Owner" {
		t.Fatalf("record result: %s, %v", raw, err)
	}
	raw = run("decision", "list", id)
	var list api.DecisionList
	if err := json.Unmarshal([]byte(raw), &list); err != nil || !reflect.DeepEqual(list.Decisions, []api.DecisionRecord{record}) {
		t.Fatalf("list result: %s, %v", raw, err)
	}
	docPath := "decisions/" + record.ID + ".json"
	raw = run("show", "--document", docPath, id)
	var doc api.Document
	var stored api.DecisionRecord
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc.Path != docPath ||
		json.Unmarshal([]byte(doc.Text), &stored) != nil || !reflect.DeepEqual(record, stored) {
		t.Fatalf("show --document result: %s, %v", raw, err)
	}
	raw = run("decision", "mode", id, "assignee")
	var changed api.DecisionModeResult
	if err := json.Unmarshal([]byte(raw), &changed); err != nil ||
		changed != (api.DecisionModeResult{TaskID: id, Mode: api.DecisionModeAssignee, Revision: card.Revision + 2}) {
		t.Fatalf("mode result: %s, %v", raw, err)
	}
	failWith := func(want string, args ...string) {
		t.Helper()
		code, stdout, stderr := commandtest.Capture(func() int {
			return cli.Run(append([]string{"kander", "--lang", "en", "--json"}, args...))
		})
		var output struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(stderr), &output); err != nil || code != 1 || output.Error.Code != want {
			t.Fatalf("kander %v: exit %d, stdout=%q, stderr=%q", args, code, stdout, stderr)
		}
	}
	failWith(api.CodeDecisionReserved, "decision", "record", id, "--kind", "leader", "--file", path)
	failWith("board.transaction_managed", "update", id, "--document", docPath, "--file", path, "--expect-revision", strconv.FormatUint(changed.Revision, 10))
}
