package tui

import (
	"context"
	"errors"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/issue"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/remote"
)

// attachRemoteBoard binds display and mutations to the REST service. The
// resource location is a label; it is never used as a filesystem board root.
func attachRemoteBoard(app *App, session *remote.Session) {
	app.cancelBoardEvents()
	app.boardEvents = boardEventWatch{seq: app.boardEvents.seq, poll: func(ctx context.Context, cursor string) (api.EventPage, error) {
		return session.Project.Events(ctx, cursor, 25*time.Second)
	}}
	app.boardRoot = session.Location("")
	app.missingBoard = false
	app.GetBoardCtx = func(ctx context.Context) (BoardPayload, error) { return session.ListView(ctx, true) }
	app.GetBoard = func() (BoardPayload, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return app.GetBoardCtx(ctx)
	}
	app.GetTaskCtx = session.TaskView
	app.GetTask = func(id string) (Task, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return session.TaskView(ctx, id)
	}
	app.LoadTaskActions = func(id string) (taskActionSource, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		task, err := session.Project.Show(ctx, id)
		if err != nil {
			return taskActionSource{}, err
		}
		localExecution := session.SceneIsLocal(task.TaskID, task.Claim, board.MetadataFrom(task.Text, board.FieldWindow), board.MetadataFrom(task.Text, board.FieldSession))
		source := taskActionSource{localExecution: localExecution, root: session.Location(""), service: session, warnings: &board.WarningLog{}, snapshot: remoteSnapshot(session, task)}
		if task.State == "backlog" {
			if problem, err := checkStartProblem(source.snapshot.Entry, task.Text); err == nil {
				source.startProblem = problem
			}
		}
		return source, nil
	}
	app.PrepareStart = func(id string) (startRequest, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		preview, err := launch.PreviewRemoteStart(ctx, session, id)
		return startRequest{StartPreview: preview, service: session}, err
	}
	app.ImportIndex = func() (issue.Index, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return issue.RemoteIndex(ctx, session)
	}
	app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
		if options.Language == "" {
			options.Language = configuredAgentLanguage()
		}
		return issue.ImportRemote(ctx, session, cli.IssueProvider(), repository, number, options)
	}
	app.LoadIssueCache = func(repository issue.Repository, number int) (issue.IssueSnapshot, bool) {
		root, err := session.LocalDataRoot()
		if err != nil {
			return issue.IssueSnapshot{}, false
		}
		return issue.ReadCachedSnapshot(root, repository, number)
	}
	app.SaveIssueCache = func(snapshot issue.IssueSnapshot) error {
		root, err := session.LocalDataRoot()
		if err != nil {
			return err
		}
		return issue.WriteCachedSnapshot(root, snapshot, issue.DefaultCacheBounds())
	}
	app.PrepareTriage = func() (launch.TriagePreview, error) { return launch.PreviewTriage("", "") }
	startIssue := func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions, refine bool) (issue.TriageOutcome, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return issue.TriageOutcome{}, err
		}
		return issue.StartRemoteTriage(ctx, session, cli.IssueProvider(), remote.ProjectDir(cwd), repository, number, options, refine)
	}
	app.TriageIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		return startIssue(ctx, repository, number, options, false)
	}
	app.RefineIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		return startIssue(ctx, repository, number, options, true)
	}
	app.ResultIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		factory := app.IssueProvider
		if factory == nil {
			factory = cli.IssueProvider
		}
		provider, ok := factory().(issue.ResultProvider)
		if !ok {
			return issue.TriageOutcome{}, errors.New(t("tui.issues_takeover_unavailable"))
		}
		root, err := session.LocalDataRoot()
		if err != nil {
			return issue.TriageOutcome{}, err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return issue.TriageOutcome{}, err
		}
		return issue.StartResult(issue.WithRemoteResult(ctx, session, remote.ProjectDir(cwd)), provider, root, repository, number, options)
	}

	app.PrepareChat = launch.PreviewChat
	app.StartChat = func(message string) (launch.ChatResult, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return launch.ChatResult{}, err
		}
		return launch.StartChat(launch.ChatRequest{Root: filepath.Join(remote.ProjectDir(cwd), "task-context"), Message: message})
	}
	app.StartTask = runTaskStart
}

func remoteSnapshot(session *remote.Session, task api.Task) board.Snapshot {
	return board.Snapshot{Entry: board.Entry{TaskID: task.TaskID, State: task.State, Kind: board.MetadataFrom(task.Text, board.FieldSize), Path: session.Location(task.TaskID), Document: session.Location(task.TaskID) + "/spec.md"}, Revision: task.Revision, Text: task.Text, OperationID: task.OperationID}
}
