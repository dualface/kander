package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIntegrationRequestWithoutContainmentKeepsItsBytes(t *testing.T) {
	legacy := `{"already_contained":true,"observation_ticket":"t","expected_revision":0,"edges":[{"ancestor":"a","descendant":"b"}],"purpose":"deliver","target_ref":"refs/heads/v2","base_commit":"b","expected_target":"b","commit":"a","execution_id":"e","actor_execution_id":"x","expected_generation":0,"decision":"d","members":[]}`
	var request IntegrationRequest
	if err := json.Unmarshal([]byte(legacy), &request); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(request)
	if err != nil || string(again) != legacy {
		t.Fatalf("legacy request changed:\n%s\n%s\n%v", legacy, again, err)
	}
	if got := request.EffectiveContainment(); got != IntegrationContained {
		t.Fatalf("legacy contained flag read as %q", got)
	}
	request.AlreadyContained = false
	if got := request.EffectiveContainment(); got != IntegrationFastForward {
		t.Fatalf("legacy request without the flag read as %q", got)
	}
}

func TestIntegrationRequestRecordsItsContainment(t *testing.T) {
	request := IntegrationRequest{Containment: IntegrationReplayed, AlreadyContained: true, Commit: "c", ExpectedTarget: "t", Replay: &IntegrationReplay{ReviewBase: "b", ReviewTarget: "c", ReplayBase: "p", ReplayTarget: "r"}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"containment":"replayed"`, `"review_base":"b"`, `"review_target":"c"`, `"replay_base":"p"`, `"replay_target":"r"`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("missing %s in %s", field, raw)
		}
	}
	if strings.Contains(string(raw), "via_commit") {
		t.Fatalf("an unused via commit was written: %s", raw)
	}
	via := IntegrationRequest{Containment: IntegrationVia, ViaCommit: "v", Commit: "c", BaseCommit: "t", ExpectedTarget: "t"}
	raw, _ = json.Marshal(via)
	if !strings.Contains(string(raw), `"via_commit":"v"`) || strings.Contains(string(raw), "replay") {
		t.Fatalf("via request: %s", raw)
	}
}

func TestIntegrationContainmentEdgesAndFinalCommit(t *testing.T) {
	base := IntegrationRequest{Commit: "c", BaseCommit: "t", ExpectedTarget: "t", ViaCommit: "v", Replay: &IntegrationReplay{ReplayTarget: "r"}}
	cases := []struct {
		kind          string
		contained     bool
		edges         []IntegrationEdge
		final, pushed string
		confirm       string
	}{
		{IntegrationFastForward, false, []IntegrationEdge{{"t", "c"}}, "c", "v", "c"},
		{IntegrationContained, true, []IntegrationEdge{{"c", "t"}}, "t", "v", "c"},
		{IntegrationVia, false, []IntegrationEdge{{"t", "v"}, {"c", "v"}}, "v", "v", "c"},
		{IntegrationReplayed, true, []IntegrationEdge{{"r", "t"}}, "t", "v", "r"},
	}
	for _, c := range cases {
		request := base
		request.Containment, request.AlreadyContained = c.kind, c.contained
		if got := request.ContainmentEdges(); len(got) != len(c.edges) || got[0] != c.edges[0] || len(got) > 1 && got[1] != c.edges[1] {
			t.Errorf("%s edges: %v", c.kind, got)
		}
		if request.FinalCommit() != c.final || request.PushCommit() != c.pushed || request.ConfirmAncestor() != c.confirm {
			t.Errorf("%s: final %s, push %s, confirm %s", c.kind, request.FinalCommit(), request.PushCommit(), request.ConfirmAncestor())
		}
	}
	plain := IntegrationRequest{Commit: "c", BaseCommit: "t", ExpectedTarget: "t"}
	if plain.PushCommit() != "c" {
		t.Fatal("a request without a carrier pushes the reviewed commit")
	}
}

func TestIntegrationViaSourceAddsOneEdgeAndOldRequestsKeepTheirBytes(t *testing.T) {
	via := IntegrationRequest{Containment: IntegrationVia, ViaCommit: "m", ViaAutoMerge: true, ViaSource: "s", Commit: "c", BaseCommit: "t", ExpectedTarget: "t"}
	want := []IntegrationEdge{{"t", "m"}, {"c", "m"}, {"s", "m"}}
	if got := via.ContainmentEdges(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("edges with a source: %v", got)
	}
	raw, _ := json.Marshal(via)
	for _, field := range []string{`"via_auto_merge":true`, `"via_source":"s"`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("missing %s in %s", field, raw)
		}
	}
	via.ViaSource = ""
	if got := via.ContainmentEdges(); len(got) != 2 {
		t.Fatalf("edges without a source: %v", got)
	}
	// A request or receipt written before the two fields reads and writes the same bytes.
	old := `{"containment":"via","via_commit":"m","observation_ticket":"","expected_revision":0,"edges":null,"purpose":"deliver","target_ref":"refs/heads/v2","base_commit":"t","expected_target":"t","commit":"c","execution_id":"x","actor_execution_id":"y","expected_generation":0,"decision":"d","members":null}`
	var request IntegrationRequest
	if err := json.Unmarshal([]byte(old), &request); err != nil {
		t.Fatal(err)
	}
	if request.ViaAutoMerge || request.ViaSource != "" {
		t.Fatalf("an old request gained fields: %+v", request)
	}
	if raw, _ := json.Marshal(request); string(raw) != old {
		t.Fatalf("an old request changed its bytes:\n%s\n%s", raw, old)
	}
}
