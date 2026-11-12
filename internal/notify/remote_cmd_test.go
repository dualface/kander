package notify

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteNotifyCommandQueuesWithoutLocalBoardOrTerminal(t *testing.T) {
	root, _ := setupBoard(t)
	t.Chdir(t.TempDir())
	forbidden := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, "notify-command"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	connection, err := client.New(httpServer.URL, "holder")
	if err != nil {
		t.Fatal(err)
	}
	info, err := connection.Register(t.Context(), cfg.Project.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	project := connection.Project(info.ID)
	card, err := project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "notify-command", Title: "Notify a remote holder", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	card, err = project.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Move(t.Context(), "", card.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "holder", ExecutionID: "notify-holder", Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	if code := RunNotify([]string{"--message", "queued remotely", card.TaskID}); code != 0 {
		t.Fatalf("remote notify failed: %d", code)
	}
	requests, err := project.Requests(t.Context(), card.TaskID)
	if err != nil || len(requests) != 1 || requests[0].Message != "queued remotely" || requests[0].Status != api.RequestOpen || requests[0].TargetGeneration != 1 {
		t.Fatalf("queued result: %+v %v", requests, err)
	}
	logs, err := filepath.Glob(filepath.Join(root, "*.log*"))
	if err != nil || len(logs) != 0 {
		t.Fatalf("command accessed a local terminal: %v %v", logs, err)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("command created a local board: %v", err)
	}
}
