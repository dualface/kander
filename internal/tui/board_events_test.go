package tui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/worker"
)

type eventFixture struct {
	app     *App
	session *remote.Session
	started chan struct{}
	fail    atomic.Bool
}

func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), TTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &eventFixture{started: make(chan struct{})}
	started := sync.OnceFunc(func() { close(fixture.started) })
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			started()
			if fixture.fail.Load() {
				http.Error(w, `{"code":"internal","message":"event endpoint unavailable"}`, http.StatusInternalServerError)
				return
			}
		}
		srv.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	connection, err := client.New(httpServer.URL, "event-host")
	if err != nil {
		t.Fatal(err)
	}
	connection.Retries = 0
	info, err := connection.Register(t.Context(), "events-fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	fixture.session = &remote.Session{Client: connection, Project: connection.Project(info.ID), Info: info, Home: t.TempDir(), ServerURL: httpServer.URL}
	fixture.app = newPanelApp(t)
	fixture.app.RefreshSecs = 3600
	attachRemoteBoard(fixture.app, fixture.session)
	initial, err := fixture.app.GetBoardCtx(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.Model.SetBoard(initial)
	fixture.app.LastRefresh = fixture.app.Now()
	t.Cleanup(fixture.app.requestQuit)
	return fixture
}
func eventMessage(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("event read was not scheduled")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("event read did not finish")
		return nil
	}
}
func applyEventSnapshot(t *testing.T, app *App) {
	t.Helper()
	cmd := app.takeBoardRead()
	if cmd == nil {
		t.Fatal("event did not schedule a board snapshot")
	}
	app.applyWork(cmd().(workMsg).payload)
}
func TestRemoteEventsWakeTheBoardBeforeItsPeriodicRefresh(t *testing.T) {
	fixture := newEventFixture(t)
	app, session := fixture.app, fixture.session
	cursor := app.Model.Cursor
	cmd := app.takeBoardEvents()
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "event-wakeup", Title: "Remote event wakeup", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	message := eventMessage(t, cmd)
	app.applyWork(message.(workMsg).payload)
	if len(app.Model.Tasks) != 0 || app.Model.Cursor != cursor {
		t.Fatal("an event directly mutated the board snapshot")
	}
	applyEventSnapshot(t, app)
	if len(app.Model.Tasks) != 1 || app.Model.Tasks[0].TaskID != task.TaskID || app.Model.Cursor == cursor {
		t.Fatal("remote change did not refresh the board")
	}
	if app.boardEvents.waitingSnapshot || app.takeBoardEvents() == nil {
		t.Fatal("event polling did not resume from the fresh snapshot")
	}
}
func TestRemoteEventResetWaitsForACompleteSnapshotCursor(t *testing.T) {
	fixture := newEventFixture(t)
	app, session := fixture.app, fixture.session
	app.Model.Cursor = "previous-service-epoch:0"
	message := eventMessage(t, app.takeBoardEvents()).(workMsg)
	page := message.payload.(boardEventResult).page
	if !page.Reset {
		t.Fatal("service did not reset the old cursor")
	}
	app.applyWork(message.payload)
	if app.Model.Cursor != "previous-service-epoch:0" || !app.boardEvents.waitingSnapshot {
		t.Fatal("reset adopted an event cursor before its snapshot")
	}
	// This change is after the reset response and must also be in the snapshot.
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "after-reset", Title: "After reset", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	applyEventSnapshot(t, app)
	if len(app.Model.Tasks) != 1 || app.Model.Tasks[0].TaskID != task.TaskID || app.Model.Cursor == page.Cursor {
		t.Fatal("reset skipped a change between response and snapshot")
	}
}
func TestEventErrorsKeepTheirBackoffAfterASuccessfulSnapshot(t *testing.T) {
	fixture := newEventFixture(t)
	app := fixture.app
	now := time.Now()
	app.Now = func() time.Time { return now }
	fixture.fail.Store(true)
	app.applyWork(eventMessage(t, app.takeBoardEvents()).(workMsg).payload)
	applyEventSnapshot(t, app)
	if app.takeBoardEvents() != nil {
		t.Fatal("a successful list erased the failed event endpoint's backoff")
	}
	now = now.Add(3 * time.Second)
	if app.takeBoardEvents() == nil {
		t.Fatal("event backoff did not allow recovery")
	}
}
func TestQuittingTheTUIDoesNotStopWorkerRenewals(t *testing.T) {
	fixture := newEventFixture(t)
	app, session := fixture.app, fixture.session
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "renew-after-ui", Title: "Keep worker ownership", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := session.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "ui-independent-worker", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, TTL: grant.TTL, RenewInterval: grant.RenewInterval, RenewedAt: time.Now()}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	payload, err := app.GetBoardCtx(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	app.Model.SetBoard(payload)
	workerCtx, stopWorker := context.WithCancel(t.Context())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- worker.Serve(workerCtx, worker.Options{Home: session.Home, Tick: 20 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Observe: func(context.Context, remote.Credential) api.AgentObservation {
			return api.AgentObservation{Status: "unknown"}
		}})
	}()
	t.Cleanup(func() {
		stopWorker()
		if err := <-workerDone; err != nil {
			t.Errorf("worker: %v", err)
		}
	})
	cmd := app.takeBoardEvents()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-fixture.started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP event poll did not start")
	}
	app.requestQuit()
	select {
	case result := <-done:
		app.applyWork(result.(workMsg).payload)
	case <-time.After(5 * time.Second):
		t.Fatal("quit did not cancel its HTTP event poll")
	}
	if app.boardReadQueued || app.takeBoardEvents() != nil {
		t.Fatal("a cancelled event result restarted UI work")
	}
	baseline, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := session.Project.Show(t.Context(), task.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Claim.RenewedAt.After(baseline.Claim.RenewedAt) {
			if current.Revision != grant.Task.Revision || current.Claim.Generation != grant.Claim.Generation || current.Claim.Status != api.ClaimActive {
				t.Fatal("UI shutdown changed ownership or card content")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("worker did not renew after the UI quit")
}
