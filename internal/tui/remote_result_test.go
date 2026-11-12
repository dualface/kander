package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/remote"
)

type resultViewService struct {
	remote.ProjectService
	state        api.IssueResultState
	reads, saves int
}

func (s *resultViewService) IssueResult(context.Context, api.IssueResultQuery) (api.IssueResultState, error) {
	s.reads++
	return s.state, nil
}
func (s *resultViewService) SaveIssueResult(_ context.Context, _ string, input api.IssueResultWrite) (api.IssueResultState, error) {
	if input.ExpectedRevision != s.state.Revision {
		return s.state, errors.New("stale fake result version")
	}
	s.saves++
	s.state.Revision++
	s.state.Store = input.Store
	return s.state, nil
}

type resultViewProvider struct {
	*fakeIssues
	value issue.ResultRemote
}

func (p *resultViewProvider) ReadResult(context.Context, issue.Repository, int) (issue.ResultRemote, error) {
	return p.value, nil
}
func (p *resultViewProvider) PostResult(context.Context, issue.Repository, int, string) (issue.ResultComment, error) {
	return issue.ResultComment{}, errors.New("unexpected publication")
}
func (p *resultViewProvider) CloseResult(context.Context, issue.Repository, int) error {
	return errors.New("unexpected closure")
}

func TestRemoteResultUIUsesServiceEvidenceAndLaunchReceipt(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	provider := &resultViewProvider{fakeIssues: newFakeIssues()}
	key, err := provider.repository.IssueSourceKey(42)
	if err != nil {
		t.Fatal(err)
	}
	id := "20261112-remote-result-task"
	spec := "# Completed\n\n- LANGUAGE: ja\n\n## SUMMARY\n\nCompleted result evidence.\n"
	work := issue.ResultWork{Cards: []issue.ResultCard{{TaskID: id, State: "done", Spec: spec, Digest: "work-digest", EvidenceVersion: "work-evidence"}}, TaskKey: id, Digest: "work-digest", EvidenceVersion: "work-evidence", Excluded: []issue.WorkCard{}}
	rawWork, err := json.Marshal(work)
	if err != nil {
		t.Fatal(err)
	}
	rawStore, err := json.Marshal(issue.ResultStore{Schema: 1, SourceKey: key, TaskID: id, Records: map[string]*issue.ResultRecord{}})
	if err != nil {
		t.Fatal(err)
	}
	service := &resultViewService{state: api.IssueResultState{Work: rawWork, Store: rawStore}}
	provider.value = issue.ResultRemote{SourceKey: key, IssueID: 42, ActorID: 7, Revision: "revision", State: "open", StateVersion: "state"}
	session := &remote.Session{Project: service, Info: api.Project{ID: "p-remote-result"}, ServerURL: "http://127.0.0.1:1", Home: t.TempDir()}
	app := newPanelApp(t)
	app.IssueProvider = func() issue.IssueProvider { return provider }
	attachRemoteBoard(app, session)
	calls := 0
	issue.SetTriageStarter(func(request issue.TriageLaunch) (issue.TriageOutcome, error) {
		calls++
		if request.CardSnapshot == nil || request.CardSnapshot.Document != spec || request.ProjectDir != cwd || request.Root == forbidden || request.Action == nil {
			t.Fatal("TUI result used local card data")
		}
		if _, err := os.Stat(request.JSONPath); err != nil {
			t.Fatal(err)
		}
		return issue.TriageOutcome{Address: "tmux:$1:@1:%1", Agent: "codex", Launcher: "tmux"}, nil
	})
	t.Cleanup(func() { issue.SetTriageStarter(launch.StartTriage) })
	options := issue.TriageOptions{CardID: id}
	for range 2 {
		value, err := app.ResultIssue(t.Context(), provider.repository, 42, options)
		if err != nil || value.Address == "" {
			t.Fatalf("remote result callback: %+v %v", value, err)
		}
	}
	if calls != 1 || service.reads == 0 || service.saves == 0 {
		t.Fatalf("result adapter/receipt: launches=%d reads=%d saves=%d", calls, service.reads, service.saves)
	}
	for _, path := range []string{forbidden, filepath.Join(cwd, "kanban")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("TUI result created local board: %v", err)
		}
	}
}
