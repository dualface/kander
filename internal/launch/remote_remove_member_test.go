package launch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

// The command takes the request file through the service with the coordinator
// credential of the group, and prints the checkpoint.
func TestRemoveMemberCommandThroughTheService(t *testing.T) {
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	cfg := config.DefaultConfig()
	cfg.Server.URL = httpServer.URL
	cfg.Project.Name = "remove-member-client"
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	group := "20261118-remove-client-group"
	var members []string
	for _, slug := range []string{"remove-one", "remove-two"} {
		task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Leave a group", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		if task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if task, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		members = append(members, task.TaskID)
	}
	grant, err := session.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-label", Basis: "confirmed removal fixture", Members: members}, "confirmed removal fixture")
	if err != nil {
		t.Fatal(err)
	}
	c := grant.Checkpoint
	path := filepath.Join(t.TempDir(), "request.json")
	b, _ := json.Marshal(board.CoordinatorRemoveMember{GroupID: group, Member: members[0], ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision", Reason: "depends on another group"})
	if err = os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	status, out := captureStdout(t, func() int { return runRemoteCoordinator(t.Context(), cfg, []string{"remove-member", path}) })
	if status != 0 {
		t.Fatalf("status %d: %s", status, out)
	}
	var printed board.CoordinatorCheckpoint
	if err = json.Unmarshal(out, &printed); err != nil || len(printed.Members) != 1 || len(printed.Removals) != 1 || printed.Revision != c.Revision+1 {
		t.Fatalf("%+v %v", printed, err)
	}
	task, err := session.Project.Show(t.Context(), members[0])
	if err != nil || board.TaskGroupFrom(task.Text) != "" {
		t.Fatalf("%+v %v", task, err)
	}
	// The same request again names the old revision.
	if status, _ = captureStdout(t, func() int { return runRemoteCoordinator(t.Context(), cfg, []string{"remove-member", path}) }); status == 0 {
		t.Fatal("a request on the old revision was accepted")
	}
	if _, err = os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("the command used the local board: %v", err)
	}
}

func TestRemoveMemberCommandRefusesAStartedMemberLocally(t *testing.T) {
	root, task, _, c := launchCoordinatorFixture(t)
	path := filepath.Join(t.TempDir(), "request.json")
	request := board.CoordinatorRemoveMember{GroupID: c.GroupID, Member: task, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Decision: "user-decision", Reason: "depends on another group"}
	b, _ := json.Marshal(request)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := RunCoordinatorRemote([]string{"remove-member", path}); status == 0 {
		t.Fatal("a started member left the group")
	}
	after, err := board.ReadCoordinatorCheckpoint(root, c.GroupID)
	if err != nil || after.Revision != c.Revision || len(after.Members) != 1 {
		t.Fatalf("%+v %v", after, err)
	}
}
