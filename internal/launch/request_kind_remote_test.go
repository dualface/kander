package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/board/boardtest"
	"github.com/dualface/kander/internal/remote"
)

type requestKindFixture struct {
	session  *remote.Session
	task     string
	reviewed boardtest.Reviewed
	evidence string
}

// newRequestKindFixture gives a claimed card with reviewed fix evidence, and
// switches the client home to another client: the one that notifies is not the
// holder.
func newRequestKindFixture(t *testing.T, slug string) requestKindFixture {
	t.Helper()
	_, session, card, _, _ := remoteLifecycleFixture(t, slug)
	grant, err := session.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "kind-holder", Owner: "claude", Start: &api.StartInput{Session: "claude kinds", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ProjectID: session.Info.ID, TaskID: card.TaskID, ClientID: session.Client.ClientID(), ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	reviewed := boardtest.ReviewedFinding(t, session.Info.BoardPath, card.TaskID)
	binding := board.DispatchFixBinding{BatchID: reviewed.BatchID, Findings: []board.DispatchFindingReference{{FindingRef: board.FindingRef{RunID: reviewed.RunID, FindingID: reviewed.FindingID}}}}
	data, err := json.Marshal(board.DispatchEvidence{Fix: &binding})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "fix-evidence.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "coordinator"))
	return requestKindFixture{session: session, task: card.TaskID, reviewed: reviewed, evidence: file}
}

func (f requestKindFixture) requests(t *testing.T) []api.CollabRequest {
	t.Helper()
	requests, err := f.session.Project.Requests(t.Context(), f.task)
	if err != nil {
		t.Fatal(err)
	}
	return requests
}

// The request kind follows the dispatch of a notify: none is a note, and a
// dispatch gives its own kind, also when only the dispatch options imply it.
func TestRemoteNotifyRequestKindFollowsTheDispatch(t *testing.T) {
	f := newRequestKindFixture(t, "notify-request-kind")
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name         string
		message      string
		options      DispatchOptions
		kind         string
		dispatchKind string
	}{
		{"plain", "The group branch advanced", DispatchOptions{}, api.RequestNote, ""},
		{"fix", "Fix the finding", DispatchOptions{ID: "kind-fix", Kind: "fix", Base: f.reviewed.Commit, EvidenceFile: f.evidence}, api.RequestFix, "fix"},
		{"default kind", "Fix by default", DispatchOptions{ID: "kind-default", Base: f.reviewed.Commit, EvidenceFile: f.evidence}, api.RequestFix, "fix"},
		{"sync", "Sync the branch", DispatchOptions{ID: "kind-sync", Kind: "sync", Base: sha}, api.RequestSync, "sync"},
		{"wrap-up", "Wrap up the delivery", DispatchOptions{ID: "kind-wrap-up", Kind: "wrap-up", Base: sha}, api.RequestWrapUp, "wrap-up"},
	} {
		request, err := NotifyRemote(f.task, tc.message, "", "", true, 120, tc.options)
		if err != nil || request.Kind != tc.kind {
			t.Errorf("%s: %+v %v", tc.name, request, err)
			continue
		}
		if tc.dispatchKind == "" && request.Dispatch != nil || tc.dispatchKind != "" && (request.Dispatch == nil || request.Dispatch.Kind != tc.dispatchKind) {
			t.Errorf("%s: dispatch %+v", tc.name, request.Dispatch)
		}
	}
}

// A non-holder dispatch prepare proposes the dispatch under the request kind of
// the same name.
func TestRemoteDispatchProposalKindEqualsTheDispatchKind(t *testing.T) {
	f := newRequestKindFixture(t, "proposal-request-kind")
	var fix board.DispatchEvidence
	data, err := os.ReadFile(f.evidence)
	if err != nil || json.Unmarshal(data, &fix) != nil {
		t.Fatalf("evidence: %v", err)
	}
	for _, tc := range []struct {
		kind, base string
		evidence   board.DispatchEvidence
	}{
		{"fix", f.reviewed.Commit, fix},
		{"sync", strings.Repeat("a", 40), board.DispatchEvidence{}},
		{"wrap-up", strings.Repeat("a", 40), board.DispatchEvidence{}},
	} {
		input := board.DispatchInput{ID: "proposal-" + tc.kind, TaskID: f.task, Kind: tc.kind, Base: tc.base, Message: "Propose " + tc.kind, Evidence: tc.evidence}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "intent.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		code, out := captureStdout(t, func() int { return RunDispatch([]string{"prepare", path}) })
		var request api.CollabRequest
		if code != 0 || json.Unmarshal(out, &request) != nil || request.Kind != tc.kind || request.Dispatch == nil || request.Dispatch.Kind != tc.kind {
			t.Errorf("%s: exit %d %s", tc.kind, code, out)
		}
	}
}

// A message the server would refuse is refused before any request exists, so a
// refusal of the older server can only mean the kind.
func TestRemoteNotifyChecksTheMessageBeforeSending(t *testing.T) {
	f := newRequestKindFixture(t, "notify-message-check")
	for name, message := range map[string]string{"blank": " \t\n ", "long": strings.Repeat("a", api.MaxRequestMessage+1)} {
		if _, err := NotifyRemote(f.task, message, "", "", true, 120, DispatchOptions{}); err == nil {
			t.Errorf("%s message was sent", name)
		}
	}
	for _, kind := range []string{"sync", "wrap-up"} {
		options := DispatchOptions{ID: "blank-" + kind, Kind: kind, Base: strings.Repeat("a", 40)}
		if _, err := NotifyRemote(f.task, "   ", "", "", true, 120, options); err == nil {
			t.Errorf("blank %s proposal was sent", kind)
		}
	}
	if requests := f.requests(t); len(requests) != 0 {
		t.Fatalf("refused messages left requests: %+v", requests)
	}
	if _, err := NotifyRemote(f.task, strings.Repeat("a", api.MaxRequestMessage), "", "", true, 120, DispatchOptions{}); err != nil {
		t.Fatalf("a message at the bound: %v", err)
	}
}

// A blank or oversized intent message is refused by a non-holder prepare
// before the request is created.
func TestRemoteDispatchProposalChecksTheMessageBeforeSending(t *testing.T) {
	f := newRequestKindFixture(t, "proposal-message-check")
	for name, message := range map[string]string{"blank": "  ", "long": strings.Repeat("a", api.MaxRequestMessage+1)} {
		raw, err := json.Marshal(board.DispatchInput{ID: "proposal-" + name, TaskID: f.task, Kind: "sync", Base: strings.Repeat("a", 40), Message: message})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "intent.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _ := captureStdout(t, func() int { return RunDispatch([]string{"prepare", path}) }); code == 0 {
			t.Errorf("%s message was proposed", name)
		}
	}
	if requests := f.requests(t); len(requests) != 0 {
		t.Fatalf("refused messages left requests: %+v", requests)
	}
}

// kander request takes the note kind and never takes wrap-up, which has no
// meaning without the dispatch evidence of notify.
func TestRequestCommandCreatesNotesAndRefusesWrapUp(t *testing.T) {
	f := newRequestKindFixture(t, "request-command-kinds")
	if code := remote.RunRequest([]string{f.task, "note", "the", "group", "advanced"}); code != 0 {
		t.Fatalf("request note: exit %d", code)
	}
	requests := f.requests(t)
	if len(requests) != 1 || requests[0].Kind != api.RequestNote || requests[0].Message != "the group advanced" || requests[0].Dispatch != nil {
		t.Fatalf("note request: %+v", requests)
	}
	if code := remote.RunRequest([]string{f.task, "wrap-up", "finish"}); code != 2 {
		t.Fatalf("request wrap-up: exit %d, want 2", code)
	}
	for _, message := range [][]string{{" ", "\t"}, {strings.Repeat("a", api.MaxRequestMessage+1)}} {
		if code := remote.RunRequest(append([]string{f.task, "note"}, message...)); code == 0 || code == 2 {
			t.Fatalf("request note %q: exit %d, want a local failure", message, code)
		}
	}
	if requests := f.requests(t); len(requests) != 1 {
		t.Fatalf("a refused request was sent: %+v", requests)
	}
	if code, out := captureStdout(t, func() int { return remote.RunRequest([]string{"show", f.task, requests[0].ID, "--json"}) }); code != 0 || !strings.Contains(string(out), `"kind":"note"`) {
		t.Fatalf("request show: exit %d %s", code, out)
	}
	if code, out := captureStdout(t, func() int { return remote.RunRequest([]string{"list", f.task}) }); code != 0 || !strings.Contains(string(out), "note") {
		t.Fatalf("request list: exit %d %s", code, out)
	}
}

// A resume of a task whose executor is not stopped turns into a notify; the
// request kind follows the dispatch options of the resume.
func TestRemoteResumeThatBecomesANotifyKeepsTheRequestKind(t *testing.T) {
	f := newRequestKindFixture(t, "resume-request-kind")
	for _, tc := range []struct {
		name string
		args []string
		kind string
	}{
		{"plain", []string{"--message", "Continue with the group branch"}, api.RequestNote},
		{"sync", []string{"--message", "Sync the branch", "--dispatch-id", "resume-sync", "--kind", "sync", "--base", strings.Repeat("a", 40)}, api.RequestSync},
		{"wrap-up", []string{"--message", "Wrap up", "--dispatch-id", "resume-wrap-up", "--kind", "wrap-up", "--base", strings.Repeat("a", 40)}, api.RequestWrapUp},
	} {
		code, out := captureStdout(t, func() int { return RunResume(append(tc.args, f.task)) })
		var request api.CollabRequest
		if code != 0 || json.Unmarshal(out, &request) != nil || request.Kind != tc.kind {
			t.Errorf("%s: exit %d %s", tc.name, code, out)
		}
	}
}
