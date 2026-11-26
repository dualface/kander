package worker

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
)

// pinnedTime is an old modification time no rewrite can produce by accident.
var pinnedTime = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// groupFixture claims a group for host-a and stores its credential with the
// given process facts. It returns the project and the credential as stored.
func groupFixture(t *testing.T, h *harness, pid int, window, session string) (*client.Project, remote.Credential) {
	t.Helper()
	project := h.project("host-a")
	group := "20261119-credential-group"
	task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "group-credential", Title: "Group credential fixture", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if task, err = project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if task, err = project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	request := api.GroupClaim{ExecutionID: "coordinator-execution", Request: board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-label", Basis: "group credential fixture", Members: []string{task.TaskID}}}
	grant, err := project.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := remote.Credential{ResourceKind: "group", ServerURL: h.http.URL, ClientID: "host-a", ProjectID: project.ID(), TaskID: group, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: grant.Claim.RenewedAt, AgentPID: pid, Window: window, Session: session}
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: "alive", Reason: "confirmed coordinator process"}
	}
	return project, c
}

// groupCredentialFile finds the credential file of an execution. It looks only
// below executions: the execution context file below sessions has the same name.
func groupCredentialFile(t *testing.T, home, execution string) string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(filepath.Join(home, "executions"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == execution+".json" {
			found = append(found, path)
		}
		return err
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("want one credential file of %s: %v %v", execution, found, err)
	}
	return found[0]
}

// pinOldTime sets an old modification time on the file and returns its bytes.
// Every rewrite replaces the file and so moves the modification time to now.
func pinOldTime(t *testing.T, path string) []byte {
	t.Helper()
	if err := os.Chtimes(path, pinnedTime, pinnedTime); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// assertUntouched fails when the file was rewritten since pinOldTime.
func assertUntouched(t *testing.T, path string, before []byte) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(pinnedTime) || !bytes.Equal(before, after) {
		t.Fatalf("the credential was rewritten: modified %s, content changed %t", info.ModTime(), !bytes.Equal(before, after))
	}
}

func TestWorkerDoesNotRewriteAnUnchangedGroupCredential(t *testing.T) {
	h := newHarness(t)
	_, c := groupFixture(t, h, 4242, "tmux:$1:@1:%1", "claude local-session")
	path := groupCredentialFile(t, h.home, c.ExecutionID)
	before := pinOldTime(t, path)
	for range 3 {
		if live, err := h.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("group pass: %d %v", live, err)
		}
	}
	assertUntouched(t, path, before)
}

func TestWorkerDoesNotRewriteALostGroupCredential(t *testing.T) {
	h := newHarness(t)
	_, c := groupFixture(t, h, 4242, "tmux:$1:@1:%1", "claude local-session")
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: "stopped", Reason: "coordinator process is gone"}
	}
	h.advance(16 * time.Second)
	if live, err := h.pass(t.Context()); err != nil || live != 0 {
		t.Fatalf("exited coordinator kept its lease: %d %v", live, err)
	}
	if got := h.credential(); !got.Lost || len(h.stopped) != 1 {
		t.Fatalf("the lost group credential must be kept and its agent stopped once: %+v %v", got, h.stopped)
	}
	path := groupCredentialFile(t, h.home, c.ExecutionID)
	before := pinOldTime(t, path)
	for range 3 {
		h.advance(16 * time.Second)
		if live, err := h.pass(t.Context()); err != nil || live != 0 {
			t.Fatalf("lost group pass: %d %v", live, err)
		}
	}
	assertUntouched(t, path, before)
	if len(h.stopped) != 1 {
		t.Fatalf("the agent of a lost group credential is stopped once: %v", h.stopped)
	}
}

func TestWorkerSavesGroupContextFactsOnce(t *testing.T) {
	h := newHarness(t)
	_, c := groupFixture(t, h, 0, "", "")
	if err := remote.SaveExecutionContext(h.home, remote.ExecutionContext{ServerURL: c.ServerURL, ClientID: c.ClientID, ProjectID: c.ProjectID, ExecutionID: c.ExecutionID, AgentPID: 4242, AgentStart: "1234", BootID: "boot-1", Window: "tmux:$1:@1:%1", Session: "claude local-session"}); err != nil {
		t.Fatal(err)
	}
	if live, err := h.pass(t.Context()); err != nil || live != 1 {
		t.Fatalf("group pass: %d %v", live, err)
	}
	got := h.credential()
	if got.AgentPID != 4242 || got.AgentStart != "1234" || got.BootID != "boot-1" || got.Window != "tmux:$1:@1:%1" || got.Session != "claude local-session" {
		t.Fatalf("the context facts were not saved into the group credential: %+v", got)
	}
	path := groupCredentialFile(t, h.home, c.ExecutionID)
	before := pinOldTime(t, path)
	for range 2 {
		if live, err := h.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("group pass: %d %v", live, err)
		}
	}
	assertUntouched(t, path, before)
}
