package tui

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteBoardReadsAndActionsWithoutLocalBoard(t *testing.T) {
	store, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	httpServer := httptest.NewServer(store.Handler())
	t.Cleanup(httpServer.Close)
	cli, err := client.New(httpServer.URL, "test-host")
	if err != nil {
		t.Fatal(err)
	}
	cli.Retries = 0
	project, err := cli.Register(t.Context(), "test", "")
	if err != nil {
		t.Fatal(err)
	}
	session := &remote.Session{Client: cli, Project: cli.Project(project.ID), Info: project, Home: t.TempDir(), ServerURL: httpServer.URL}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "remote-tui", Title: "Remote task", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "confirmed")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	app := newPanelApp(t)
	attachRemoteBoard(app, session)
	payload, err := app.GetBoardCtx(t.Context())
	if err != nil || len(payload.Tasks) != 1 || payload.Stale {
		t.Fatalf("server board: %+v %v", payload, err)
	}
	detail, err := app.GetTaskCtx(t.Context(), task.TaskID)
	if err != nil || detail.Document != text {
		t.Fatalf("server detail: %+v %v", detail, err)
	}
	source, err := app.LoadTaskActions(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runTaskAction(source, actionPick, board.MoveOptions{}); err != nil {
		t.Fatal(err)
	}
	// The dialog's captured revision must fence a later action.
	if _, _, err := runTaskAction(source, actionBacklog, board.MoveOptions{}); !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("stale action was not rejected: %v", err)
	}
	payload, err = app.GetBoardCtx(t.Context())
	if err != nil || payload.Tasks[0].State != "todo" {
		t.Fatalf("updated server state: %+v %v", payload, err)
	}
	httpServer.Close()
	stale, err := app.GetBoardCtx(t.Context())
	if err != nil || !stale.Stale || len(stale.Warnings) == 0 || stale.Tasks[0].State != "todo" {
		t.Fatalf("offline snapshot must be visibly stale: %+v %v", stale, err)
	}
	app.Model.SetBoard(stale)
	if !app.Model.Stale {
		t.Fatal("model discarded offline status")
	}
	if _, _, err := runTaskAction(source, actionBacklog, board.MoveOptions{}); err == nil {
		t.Fatal("offline action succeeded")
	}
}
