package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

func TestAttachBoardUsesSummaryCacheAndQuitClosesIt(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := board.NewTask(root, "feature", "cache-bind", "Cache bind", "en", false); err != nil {
		t.Fatal(err)
	}
	app := newApp(true, 30, pageContext{}, nil, nil, "auto", 40, nil, nil)
	attachBoard(app, root)
	if app.summaries == nil {
		t.Fatal("attachBoard did not create a summary index")
	}
	first, err := app.GetBoard()
	if err != nil || len(first.Tasks) != 1 {
		t.Fatalf("first GetBoard: %+v %v", first, err)
	}
	if got := app.summaries.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("first stats %+v", got)
	}
	second, err := app.GetBoardCtx(context.Background())
	if err != nil || len(second.Tasks) != 1 {
		t.Fatalf("hot GetBoardCtx: %+v %v", second, err)
	}
	if got := app.summaries.Stats(); got.DocumentReads != 0 || got.Parses != 0 || !got.Reused {
		t.Fatalf("hot stats %+v", got)
	}
	index := app.summaries
	app.requestQuit()
	if app.summaries != nil {
		t.Fatal("quit left the index open")
	}
	if _, err := index.View(context.Background()); err == nil {
		t.Fatal("closed index still served views")
	}
}

func TestTaskActionPickInvalidatesSummaryCache(t *testing.T) {
	source := actionTestSource(t, "backlog", true)
	app := actionTestApp(t, source)
	attachBoard(app, source.root)
	app.LoadTaskActions = func(id string) (taskActionSource, error) {
		snapshot, err := board.ReadSnapshot(source.root, id)
		return taskActionSource{root: source.root, snapshot: snapshot}, err
	}
	if _, err := app.GetBoard(); err != nil {
		t.Fatal(err)
	}
	chooseTaskAction(t, app, actionPick)
	runPendingWork(t, app)
	if app.Model.Tasks[0].State != "todo" {
		t.Fatalf("pick did not refresh from cache: %+v", app.Model.Tasks)
	}
	if got := app.summaries.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("pick did not reread the moved card: %+v", got)
	}
}

func TestStartResultInvalidatesSummaryCache(t *testing.T) {
	source := actionTestSource(t, "backlog", true)
	app := actionTestApp(t, source)
	attachBoard(app, source.root)
	if _, err := app.GetBoard(); err != nil {
		t.Fatal(err)
	}
	id := source.snapshot.Entry.TaskID
	app.applyStartResult(confirmWork{payload: launch.StartResult{TaskID: id}, err: errors.New("start failed")})
	if _, err := app.GetBoard(); err != nil {
		t.Fatal(err)
	}
	if got := app.summaries.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("start failure did not invalidate: %+v", got)
	}
}

func TestSyncSummaryStrongIntervalFollowsRefreshSecs(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := board.NewTask(root, "chore", "refresh-interval", "Refresh interval", "en", false); err != nil {
		t.Fatal(err)
	}
	app := newApp(true, 3600, pageContext{}, nil, nil, "auto", 40, nil, nil)
	attachBoard(app, root)
	if app.summaries == nil {
		t.Fatal("missing summary index")
	}
	if got, want := app.summaries.StrongEvery(), board.StrongInterval(3600); got != want {
		t.Fatalf("initial strong interval %s want %s", got, want)
	}
	app.RefreshSecs = 1
	app.syncSummaryStrongInterval()
	if got, want := app.summaries.StrongEvery(), board.StrongInterval(1); got != want {
		t.Fatalf("updated strong interval %s want %s", got, want)
	}
}
