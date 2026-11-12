package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func openTest(t *testing.T, dir string, now func() time.Time) (*Server, *httptest.Server) {
	t.Helper()
	s, err := Open(Options{DataDir: dir, Now: now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	return s, h
}

// prepareTodo registers the project and publishes one todo task.
func prepareTodo(t *testing.T, url string) (*client.Project, string) {
	t.Helper()
	c, _ := client.New(url, "host-a")
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "crash", Title: "Crash", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if task, err = p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	return p, task.TaskID
}

// A crash after the board move of a claim but before its activation leaves a
// pending claim and a pending receipt. Startup activates the claim from the
// card state, and the client's retry with the same key returns the grant
// instead of a second claim or a conflict.
func TestClaimInterruptedAfterTheMoveRecovers(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	req := api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude"}
	r := claimRecord{TaskID: id, ClientID: req.ClientID, ExecutionID: req.ExecutionID, Owner: req.Owner, Generation: 1, Status: claimPending}
	r.TokenHash = tokenHash(s.token(proj.info.ID, id, r.ExecutionID, 1))
	if err := proj.writeClaim(r); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(proj.root, receiptPath("key-crash"), receipt{Key: "key-crash", Operation: "claim:" + id, Digest: requestDigest("claim:"+id, req), Status: receiptPending}); err != nil {
		t.Fatal(err)
	}
	view, _ := board.LoadBoard(proj.board)
	entry, _ := board.Locate(view, id)
	if _, err := board.MoveWithOptions(entry, proj.board, "working", board.MoveOptions{Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	s.Close()

	s, h = openTest(t, dir, nil)
	defer s.Close()
	defer h.Close()
	c, _ := client.New(h.URL, "host-a")
	p = c.Project(p.ID())
	shown, err := p.Show(t.Context(), id)
	if err != nil || shown.Claim == nil || shown.Claim.Status != api.ClaimActive {
		t.Fatalf("startup must activate the committed claim: %+v %v", shown.Claim, err)
	}
	g, err := p.Claim(t.Context(), "key-crash", id, req)
	if err != nil || g.Claim.Generation != 1 || g.Token == "" {
		t.Fatalf("the retried claim must return the recovered grant: %+v %v", g, err)
	}
}

// A crash before the board move leaves a pending claim of a todo card; startup
// releases it, so the task stays claimable.
func TestClaimInterruptedBeforeTheMoveIsReleased(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	if err := proj.writeClaim(claimRecord{TaskID: id, ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude", Generation: 1, Status: claimPending}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	s.Close()
	s, h = openTest(t, dir, nil)
	defer s.Close()
	defer h.Close()
	proj, _ = s.project(p.ID())
	r, _, err := proj.readClaim(id)
	if err != nil || r.Status != api.ClaimReleased {
		t.Fatalf("an uncommitted claim must be released: %+v %v", r, err)
	}
	c, _ := client.New(h.URL, "host-b")
	g, err := c.Project(p.ID()).Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-b", ExecutionID: "exec-b", Owner: "codex"})
	if err != nil || g.Claim.Generation != 2 {
		t.Fatalf("the task must stay claimable with the next generation: %+v %v", g.Claim, err)
	}
}

// A move that committed before its receipt was finished is answered from the
// card state on retry, not refused as a revision conflict.
func TestMoveRetryAfterLostAnswer(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	shown, _ := p.Show(t.Context(), id)
	rev := shown.Revision
	req := api.Move{State: "backlog", ExpectedRevision: &rev}
	if err := writeJSON(proj.root, receiptPath("key-move"), receipt{Key: "key-move", Operation: "move:" + id, Digest: requestDigest("move:"+id, req), Status: receiptPending}); err != nil {
		t.Fatal(err)
	}
	view, _ := board.LoadBoard(proj.board)
	entry, _ := board.Locate(view, id)
	if _, err := board.MoveWithOptions(entry, proj.board, "backlog", board.MoveOptions{}); err != nil {
		t.Fatal(err)
	}
	moved, err := p.Move(t.Context(), "key-move", id, req, nil)
	if err != nil || moved.State != "backlog" {
		t.Fatalf("retry must report the committed move: %+v %v", moved, err)
	}
}

// A committed termination whose claim release was lost is settled at startup.
func TestStartupReleasesClaimsOfFinishedCards(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	p, id := prepareTodo(t, h.URL)
	if _, err := p.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-1", Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	proj, _ := s.project(p.ID())
	view, _ := board.LoadBoard(proj.board)
	entry, _ := board.Locate(view, id)
	if _, err := board.MoveWithOptions(entry, proj.board, "trash", board.MoveOptions{Result: "trashed", Reason: "test", Decision: "test"}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	s.Close()
	s, h = openTest(t, dir, nil)
	defer s.Close()
	defer h.Close()
	proj, _ = s.project(p.ID())
	r, _, err := proj.readClaim(id)
	if err != nil || r.Status != api.ClaimReleased {
		t.Fatalf("claim of a trashed card must be released: %+v %v", r, err)
	}
}

// The lease and receipt must belong to the board transaction, rather than
// depending on a later best-effort write by the HTTP handler.
func TestClaimAndTakeoverPersistInTheCardJournal(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().UTC()
	s, h := openTest(t, dir, func() time.Time { return at })
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	request := api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex"}
	grant, err := p.Claim(t.Context(), "atomic-claim", id, request)
	if err != nil {
		t.Fatal(err)
	}
	assertTransaction := func(key string) {
		t.Helper()
		execution, _, err := board.CurrentExecution(proj.board, id)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(proj.board, ".kander", "operations", "committed", execution.ID+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record board.OperationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		claim, receipt := false, false
		for _, change := range record.Files {
			claim = claim || filepath.Base(change.Path) == id+".json" && strings.Contains(change.Path, "claims")
			receipt = receipt || filepath.Base(change.Path) == filepath.Base(receiptPath(key)) && strings.Contains(change.Path, "receipts")
		}
		if !claim || !receipt {
			t.Fatalf("card transaction omitted claim or receipt: %+v", record.Files)
		}
	}
	assertTransaction("atomic-claim")
	at = at.Add(2 * time.Minute)
	request.ExecutionID, request.ExpectedGeneration, request.Decision = "exec-b", grant.Claim.Generation, "user takeover"
	if _, err := p.Takeover(t.Context(), "atomic-takeover", id, request); err != nil {
		t.Fatal(err)
	}
	assertTransaction("atomic-takeover")
}

func TestReleasedExecutionCannotModifyUnclaimedCard(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	p, id := prepareTodo(t, h.URL)
	grant, err := p.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex", Start: &api.StartInput{Session: "codex pending"}})
	if err != nil {
		t.Fatal(err)
	}
	own := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := p.Execution(t.Context(), "", id, api.ExecutionUpdate{Action: api.ExecutionRollback}, own); err != nil {
		t.Fatal(err)
	}
	current, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.PutDocument(t.Context(), "late-write", id, api.Document{Path: "report.md", Text: "stale result", ExpectedRevision: current.Revision}, &own)
	if !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("released execution wrote to an unclaimed task: %v", err)
	}
	if _, err := p.Document(t.Context(), id, "report.md"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("stale report became visible: %v", err)
	}
}
