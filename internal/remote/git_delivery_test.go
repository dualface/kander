package remote_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func deliveryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git %v: %s: %v", args, data, err)
	}
	return strings.TrimSpace(string(data))
}

func TestDeliveryCrossesIndependentClonesAndMissingRemoteBlocks(t *testing.T) {
	root := t.TempDir()
	bare, first, second := filepath.Join(root, "common.git"), filepath.Join(root, "first"), filepath.Join(root, "second")
	deliveryGit(t, root, "init", "--bare", bare)
	deliveryGit(t, root, "clone", bare, first)
	if err := os.WriteFile(filepath.Join(first, "work.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, first, "add", "work.txt")
	deliveryGit(t, first, "commit", "-m", "Initial work")
	deliveryGit(t, first, "push", "origin", "HEAD:refs/heads/v2")
	deliveryGit(t, root, "clone", "--branch", "v2", bare, second)
	if err := os.WriteFile(filepath.Join(first, "work.txt"), []byte("delivered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, first, "commit", "-am", "Deliver work")
	commit := deliveryGit(t, first, "rev-parse", "HEAD")
	if deliveryGit(t, first, "rev-parse", "--path-format=absolute", "--git-common-dir") == deliveryGit(t, second, "rev-parse", "--path-format=absolute", "--git-common-dir") {
		t.Fatal("clones share Git object storage")
	}
	if err := exec.CommandContext(t.Context(), "git", "-C", second, "cat-file", "-e", commit+"^{commit}").Run(); err == nil {
		t.Fatal("second clone already contains the private commit")
	}
	service, err := server.Open(server.Options{DataDir: filepath.Join(root, "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	httpServer := httptest.NewServer(service.Handler())
	t.Cleanup(httpServer.Close)
	a, _ := client.New(httpServer.URL, "host-a")
	b, _ := client.New(httpServer.URL, "host-b")
	info, err := a.Register(t.Context(), "clone-delivery", bare)
	if err != nil {
		t.Fatal(err)
	}
	sa := &remote.Session{Client: a, Project: a.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(root, "client first")}
	sb := &remote.Session{Client: b, Project: b.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(root, "client second")}
	for _, session := range []*remote.Session{sa, sb} {
		if err := os.MkdirAll(session.Home, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	card, err := sa.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "clone-delivery", Title: "Verify independent clone delivery", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	card, err = sa.Project.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	card, err = sa.Project.Move(t.Context(), "", card.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := sa.Project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "clone-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: httpServer.URL, ClientID: "host-a", ProjectID: info.ID, TaskID: card.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	delivery, err := sa.PublishCommit(t.Context(), first, card.TaskID, commit, credential)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := sa.PublishCommit(t.Context(), first, card.TaskID, commit, credential)
	if err != nil || !replay.ObservedAt.Equal(delivery.ObservedAt) {
		t.Fatalf("publication replay: %+v %v", replay, err)
	}
	if _, err := sb.FetchDelivery(t.Context(), second, card.TaskID, commit); err != nil {
		t.Fatal(err)
	}
	if got := deliveryGit(t, second, "show", commit+":work.txt"); got != "delivered" {
		t.Fatal(got)
	}
	firstWorktree, err := sa.PrepareWorktree(t.Context(), first, commit, credential)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstWorktree.Path, "work.txt"), []byte("uncommitted predecessor work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repeated, err := sa.PrepareWorktree(t.Context(), first, commit, credential)
	if err != nil || repeated.Path != firstWorktree.Path {
		t.Fatalf("worktree retry: %+v %v", repeated, err)
	}
	if _, err := a.Project(info.ID).ObserveAgent(t.Context(), "confirmed-exit", card.TaskID, api.AgentObservation{Status: "stopped", Reason: "explicit fixture exit"}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	state, err := sa.Project.ExecutionState(t.Context(), card.TaskID, *credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sa.Project.Execution(t.Context(), "release-source", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "source holder releases for explicit clone takeover", Presence: &api.AgentObservation{Status: "stopped"}}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	successor, err := sb.Project.Takeover(t.Context(), "takeover-new-clone", card.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "clone-successor", Owner: "codex", Decision: "explicit takeover in second independent clone", ExpectedGeneration: credential.Generation})
	if err != nil {
		t.Fatal(err)
	}
	next := remote.Credential{ServerURL: httpServer.URL, ClientID: "host-b", ProjectID: info.ID, TaskID: card.TaskID, ExecutionID: successor.Claim.ExecutionID, Generation: successor.Claim.Generation, Token: successor.Token}
	nextWorktree, err := sb.PrepareWorktree(t.Context(), second, commit, next)
	if err != nil {
		t.Fatal(err)
	}
	if nextWorktree.Branch == firstWorktree.Branch || nextWorktree.Path == firstWorktree.Path {
		t.Fatal("takeover reused the predecessor's execution workspace")
	}
	if deliveryGit(t, nextWorktree.Path, "rev-parse", "HEAD") != commit {
		t.Fatal("successor did not start at the fetched delivery")
	}
	oldWork, err := os.ReadFile(filepath.Join(firstWorktree.Path, "work.txt"))
	if err != nil || string(oldWork) != "uncommitted predecessor work\n" {
		t.Fatalf("takeover lost predecessor work: %q %v", oldWork, err)
	}
	if _, err := sa.PrepareWorktree(t.Context(), first, commit, credential); err == nil {
		t.Fatal("old execution provisioned after takeover")
	}
	deliveryGit(t, second, "push", "origin", ":"+delivery.Ref)
	if _, err := sb.FetchDelivery(t.Context(), second, card.TaskID, commit); err == nil {
		t.Fatal("cached object concealed a missing remote delivery")
	}
	if _, err := os.Stat(filepath.Join(first, "kanban")); !os.IsNotExist(err) {
		t.Fatalf("client created local board: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "kanban")); !os.IsNotExist(err) {
		t.Fatalf("client created local board: %v", err)
	}
	if delivery.ObservedAt.After(time.Now()) {
		t.Fatal("invalid observation time")
	}
}
