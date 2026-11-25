package launch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/board/boardtest"
	"github.com/dualface/kander/internal/remote"
)

// A coordinator whose fix evidence is wrong learns it from the notify itself:
// the command fails with the list of problems, the server keeps no proposal,
// and the corrected evidence sent again without a dispatch ID creates one.
func TestRemoteNotifyRefusesWrongFixEvidenceAndAcceptsTheCorrection(t *testing.T) {
	_, session, card, _, _ := remoteLifecycleFixture(t, "remote-fix-evidence")
	grant, err := session.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "fix-evidence-holder", Owner: "claude", Start: &api.StartInput{Session: "claude fix evidence", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ProjectID: session.Info.ID, TaskID: card.TaskID, ClientID: session.Client.ClientID(), ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if session.Info.BoardPath == "" {
		t.Fatal("the session does not name the board of the test server")
	}
	reviewed := boardtest.ReviewedFinding(t, session.Info.BoardPath, card.TaskID)
	// The coordinator is another client of the project.
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "coordinator"))

	evidence := func(previous string) string {
		t.Helper()
		binding := board.DispatchFixBinding{BatchID: reviewed.BatchID, Findings: []board.DispatchFindingReference{{FindingRef: board.FindingRef{RunID: reviewed.RunID, FindingID: reviewed.FindingID}, PreviousRunID: previous}}}
		data, err := json.Marshal(board.DispatchEvidence{Fix: &binding})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	file := filepath.Join(t.TempDir(), "evidence.json")
	write := func(previous string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(evidence(previous)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	notify := func() (api.CollabRequest, error) {
		return NotifyRemote(card.TaskID, "Fix the reviewed finding", "", "", true, 120, DispatchOptions{Kind: "fix", Base: reviewed.Commit, EvidenceFile: file})
	}

	write("earlier")
	_, err = notify()
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != api.CodeBoardRejected || refusal.BoardCode != "board.dispatch_evidence_invalid" {
		t.Fatalf("wrong evidence: %v", err)
	}
	for _, want := range []string{reviewed.RunID + "/PM-01", "previous_run_id", `"earlier"`, "kander notify"} {
		if !strings.Contains(refusal.Message, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, refusal.Message)
		}
	}
	requests, err := session.Project.Requests(t.Context(), card.TaskID)
	if err != nil || len(requests) != 0 {
		t.Fatalf("a refused notify left proposals: %+v %v", requests, err)
	}

	write("")
	request, err := notify()
	if err != nil || request.Status != api.RequestOpen || request.Dispatch == nil || request.Dispatch.Kind != "fix" {
		t.Fatalf("corrected notify: %+v %v", request, err)
	}
	if requests, err = session.Project.Requests(t.Context(), card.TaskID); err != nil || len(requests) != 1 {
		t.Fatalf("proposals after the correction: %+v %v", requests, err)
	}
}
