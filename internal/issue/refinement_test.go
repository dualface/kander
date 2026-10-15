package issue

import (
	"errors"
	"github.com/dualface/kander/internal/board"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRefinementUsesBoundBacklogSnapshotWithoutFetching(t *testing.T) {
	root := importTestBoard(t)
	repo := importTestRepository()
	provider := &stubResolver{snapshot: importTestSnapshot(repo, 42)}
	imported, err := Import(t.Context(), provider, root, repo, 42, ImportOptions{Language: "ja", Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(imported.Path, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	old := triageStarter
	t.Cleanup(func() { triageStarter = old })
	calls := 0
	triageStarter = func(request TriageLaunch) (TriageOutcome, error) {
		calls++
		if !request.RefineCard || request.ResultSync || request.CardID != imported.TaskID || request.JSONPath != filepath.Join(imported.Path, SourceFileName) || request.MarkdownPath != filepath.Join(imported.Path, SourceMarkdownName) || request.Agent != "claude" || request.Launcher != "tmux" {
			t.Fatalf("request=%+v", request)
		}
		return TriageOutcome{}, errors.New("launch failed")
	}
	_, err = StartRefinement(t.Context(), root, repo, 42, TriageOptions{CardID: imported.TaskID, Agent: "claude", Launcher: "tmux"})
	if err == nil || calls != 1 || provider.issueCalls != 1 {
		t.Fatalf("calls=%d fetches=%d err=%v", calls, provider.issueCalls, err)
	}
	after, err := os.ReadFile(filepath.Join(imported.Path, "spec.md"))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed launch changed card", err)
	}
	for _, id := range []string{"", "other-card"} {
		if _, err := StartRefinement(t.Context(), root, repo, 42, TriageOptions{CardID: id}); err == nil {
			t.Fatal("accepted wrong card", id)
		}
	}
	if calls != 1 {
		t.Fatal("invalid target launched")
	}
	view, err := board.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = board.MoveWithOptions(view.Entries[imported.TaskID], root, "trash", board.MoveOptions{Result: "trashed", Reason: "test", Decision: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartRefinement(t.Context(), root, repo, 42, TriageOptions{CardID: imported.TaskID}); err == nil {
		t.Fatal("non-backlog card accepted")
	}
	if calls != 1 {
		t.Fatal("non-backlog card launched")
	}
}
