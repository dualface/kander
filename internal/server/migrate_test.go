package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// v1Board builds a v1 board with a backlog card and a working card.
func v1Board(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "kanban")
	if err := board.InitStorage(root); err != nil {
		t.Fatal(err)
	}
	if _, err := board.NewTask(root, "feature", "idle", "Idle", "en", false); err != nil {
		t.Fatal(err)
	}
	target, err := board.NewTask(root, "feature", "busy", "Busy", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(target)
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(s.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if err := board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"todo", "working"} {
		view, _ := board.LoadBoard(root)
		entry, _ := board.Locate(view, id)
		options := board.MoveOptions{}
		if state == "working" {
			options.Owner = "claude"
		}
		if _, err := board.MoveWithOptions(entry, root, state, options); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestImportFillsAnEmptyProjectAndKeepsTheSource(t *testing.T) {
	source := v1Board(t)
	_, before, err := snapshotTree(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	defer h.Close()
	defer s.Close()
	result, err := s.Import("demo", "git@example.invalid:demo.git", source)
	if err != nil || result.Tasks != 2 || result.Existing {
		t.Fatalf("import: %+v %v", result, err)
	}
	if _, after, _ := snapshotTree(source); after != before {
		t.Fatal("the source board must stay byte-identical")
	}
	again, err := s.Import("demo", "git@example.invalid:demo.git", source)
	if err != nil || !again.Existing || again.Snapshot != result.Snapshot {
		t.Fatalf("the same snapshot imports once: %+v %v", again, err)
	}
	if _, err := board.NewTask(source, "chore", "later", "Later", "en", false); err != nil {
		t.Fatal(err)
	}
	_, err = s.Import("demo", "git@example.invalid:demo.git", source)
	if !api.IsCode(err, api.CodeProjectNotEmpty) {
		t.Fatalf("a changed board never merges into a project with tasks: %v", err)
	}

	c, _ := client.New(h.URL, "host-b")
	p := c.Project(result.Project.ID)
	list, err := p.List(t.Context(), "")
	if err != nil || len(list.Tasks) != 2 {
		t.Fatalf("imported tasks: %+v %v", list, err)
	}
	var busy string
	for _, task := range list.Tasks {
		if task.State == "working" {
			busy = task.TaskID
		}
	}
	shown, err := p.Show(t.Context(), busy)
	if err != nil || shown.Claim != nil {
		t.Fatalf("an imported working card has no claim: %+v %v", shown.Claim, err)
	}
	_, err = p.Move(t.Context(), "", busy, api.Move{State: "done", Result: "completed"}, nil)
	expectServerCode(t, err, api.CodeLeaseInvalid)
	g, err := p.Takeover(t.Context(), "", busy, api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b", Owner: "codex", Decision: "user:migration"})
	if err != nil || g.Claim.Generation != 1 {
		t.Fatalf("an imported working card is taken over from generation 0: %+v %v", g.Claim, err)
	}
}

func TestImportRefusesASecondInstance(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	defer h.Close()
	defer s.Close()
	if _, err := Open(Options{DataDir: dir}); err == nil {
		t.Fatal("an import must not run beside a serving instance")
	}
}

func expectServerCode(t *testing.T, err error, code string) {
	t.Helper()
	if !api.IsCode(err, code) {
		t.Fatalf("want %s, got %v", code, err)
	}
}
