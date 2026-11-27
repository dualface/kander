package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// contentKey has the shape of the worker's content key for a managed answer:
// a key anyone who reads the request can compute before the holder sends it.
const contentKey = "answer-0123456789abcdef0123456789abcdef"

// scopeFixture is a project with one claimed task and an open request to its
// holder.
type scopeFixture struct {
	server  *Server
	http    *httptest.Server
	project *project
	holder  *client.Project
	other   *client.Project
	task    string
	proof   client.Claim
	request api.CollabRequest
}

func newScopeFixture(t *testing.T, hooks Hooks) *scopeFixture {
	t.Helper()
	s, err := Open(Options{DataDir: t.TempDir(), Hooks: hooks, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(attributedHandler(s.Handler()))
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	holder, task := prepareTodo(t, h.URL)
	proj, err := s.project(holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := holder.Claim(t.Context(), "", task, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-a", Owner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New(h.URL, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	other := c.Project(holder.ID())
	request, err := other.CreateRequest(t.Context(), "", task, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix the failing case", TargetGeneration: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	return &scopeFixture{server: s, http: h, project: proj, holder: holder, other: other, task: task, proof: client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}, request: request}
}

// answer is the holder's managed answer to the open request.
func (f *scopeFixture) answer(note string) api.CollabAnswer {
	return api.CollabAnswer{ExpectedRevision: f.request.Revision, Status: api.RequestDone, Note: note}
}

// post sends one write as raw HTTP and returns the status and the exact body.
// An empty token sends no claim headers.
func (f *scopeFixture) post(t *testing.T, path, key, token string, generation uint64, body any, header ...string) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.http.URL+"/api/v1/projects/"+f.holder.ID()+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderClientID, "host-raw")
	req.Header.Set(api.HeaderIdempotencyKey, key)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(api.HeaderClaimToken, token)
		req.Header.Set(api.HeaderClaimGen, strconv.FormatUint(generation, 10))
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func (f *scopeFixture) answerPath() string {
	return "/tasks/" + f.task + "/requests/" + f.request.ID + "/answer"
}

// stored is the request record the server holds now.
func (f *scopeFixture) stored(t *testing.T) api.CollabRequest {
	t.Helper()
	requests, err := f.holder.Requests(t.Context(), f.task)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range requests {
		if r.ID == f.request.ID {
			return r
		}
	}
	t.Fatalf("request %s is gone: %+v", f.request.ID, requests)
	return api.CollabRequest{}
}

func errorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e api.Error
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("error body %s: %v", raw, err)
	}
	return e.Code
}

// B1: a caller without the claim token who writes the holder's predictable
// key on another route first no longer turns the holder's answer into
// idempotency_mismatch (Security-001).
func TestReceiptScopeKeepsAnotherRouteFromTakingTheHoldersKey(t *testing.T) {
	f := newScopeFixture(t, Hooks{})
	if _, err := f.other.CreateRequest(t.Context(), contentKey, f.task, api.NewCollabRequest{Kind: api.RequestNote, Message: "taken first", TargetGeneration: f.proof.Generation}); err != nil {
		t.Fatalf("the earlier write on the other route: %v", err)
	}
	answered, err := f.holder.AnswerRequest(t.Context(), contentKey, f.task, f.request.ID, f.answer("fixed and verified"), f.proof)
	if err != nil || answered.Status != api.RequestDone || answered.Note != "fixed and verified" {
		t.Fatalf("holder answer = %+v, %v", answered, err)
	}
	if stored := f.stored(t); stored.Note != "fixed and verified" {
		t.Fatalf("request record lost the note: %+v", stored)
	}
}

// B2 and B2b: a caller without the token, or with a token of its own, that
// sends the holder's key and body to the same route gets its own refusal; the
// holder's later answer runs instead of replaying that refusal.
func TestReceiptScopeKeepsAnotherCallersRefusalFromTheHolder(t *testing.T) {
	for name, token := range map[string]string{
		"no token":    "",
		"wrong token": "f00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeed",
	} {
		t.Run(name, func(t *testing.T) {
			f := newScopeFixture(t, Hooks{})
			answer := f.answer("fixed and verified")
			status, raw := f.post(t, f.answerPath(), contentKey, token, f.proof.Generation, answer)
			if code := errorCode(t, raw); status < 400 || !slices.Contains(claimRefusals, code) {
				t.Fatalf("earlier answer = %d %s, want a claim refusal", status, raw)
			}
			answered, err := f.holder.AnswerRequest(t.Context(), contentKey, f.task, f.request.ID, answer, f.proof)
			if err != nil || answered.Status != api.RequestDone || answered.Note != "fixed and verified" {
				t.Fatalf("holder answer = %+v, %v; the earlier refusal was replayed", answered, err)
			}
		})
	}
}

// claimRefusals are the codes a write gets for credentials that do not hold
// the claim.
var claimRefusals = []string{api.CodeClaimRequired, api.CodeHeldByOther, api.CodeLeaseInvalid, api.CodeTakenOver}

// B3 and B9: the holder's retry of the same key and body replays the first
// answer byte for byte and applies it once; no receipt holds the token.
func TestReceiptScopeReplaysTheSameCallersRetry(t *testing.T) {
	f := newScopeFixture(t, Hooks{})
	cursor := f.project.events.cursor()
	answer := f.answer("fixed and verified")
	status, first := f.post(t, f.answerPath(), contentKey, f.proof.Token, f.proof.Generation, answer)
	if status != http.StatusOK {
		t.Fatalf("first answer = %d %s", status, first)
	}
	again, replay := f.post(t, f.answerPath(), contentKey, f.proof.Token, f.proof.Generation, answer)
	if again != status || !bytes.Equal(replay, first) {
		t.Fatalf("retry = %d %s, want %d %s", again, replay, status, first)
	}
	if stored := f.stored(t); stored.Revision != f.request.Revision+1 || stored.Note != answer.Note {
		t.Fatalf("request applied %d times: %+v", stored.Revision-f.request.Revision, stored)
	}
	page := f.project.events.read(t.Context(), cursor, 0)
	written := 0
	for _, event := range page.Events {
		if event.Kind == api.EventRequest && event.RequestID == f.request.ID {
			written++
		}
	}
	if written != 1 {
		t.Fatalf("request events = %d, want 1: %+v", written, page.Events)
	}
	err := filepath.WalkDir(filepath.Join(f.project.root, servicePath("receipts")), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), f.proof.Token) {
			t.Errorf("receipt %s holds the claim token", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// B4: inside one scope a key reused with another body is still refused.
func TestReceiptScopeRefusesAnotherBodyInTheSameScope(t *testing.T) {
	f := newScopeFixture(t, Hooks{})
	if _, err := f.holder.AnswerRequest(t.Context(), contentKey, f.task, f.request.ID, f.answer("fixed and verified"), f.proof); err != nil {
		t.Fatal(err)
	}
	status, raw := f.post(t, f.answerPath(), contentKey, f.proof.Token, f.proof.Generation, f.answer("another note"))
	if status != http.StatusUnprocessableEntity || errorCode(t, raw) != api.CodeIdempotencyMismatch {
		t.Fatalf("other body = %d %s, want 422 idempotency_mismatch", status, raw)
	}
}

// callerHeader carries the caller a test host attributes a request to.
const callerHeader = "X-Test-Caller"

func attributedHooks() Hooks {
	return Hooks{Attribution: func(ctx context.Context) (string, bool) {
		// The fixture's own setup sends no test caller; it is the owner.
		if caller, _ := ctx.Value(testCallerKey{}).(string); caller != "" {
			return caller, true
		}
		return "owner", true
	}}
}

type testCallerKey struct{}

// attributedHandler puts the caller of the test header into the context the
// attribution hook reads, as an embedding host's authentication would.
func attributedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), testCallerKey{}, r.Header.Get(callerHeader))))
	})
}

// B10: the request ID derives from the key alone. Another attributed caller
// that sends the key of an existing request, with another body or the same
// one, is refused and the request stays as it was.
func TestReceiptScopeKeepsAKeyDerivedRequestFromAnotherScope(t *testing.T) {
	f := newScopeFixture(t, attributedHooks())
	path := "/tasks/" + f.task + "/requests"
	body := api.NewCollabRequest{Kind: api.RequestNote, Message: "from A", FromClient: "host-a", TargetGeneration: f.proof.Generation}
	status, raw := f.post(t, path, "shared-request-key", "", 0, body, callerHeader, "alice")
	if status != http.StatusCreated {
		t.Fatalf("A's request = %d %s", status, raw)
	}
	var created api.CollabRequest
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	other := body
	other.Message, other.FromClient = "from B", "host-b"
	for _, b := range []api.NewCollabRequest{other, body} {
		status, raw := f.post(t, path, "shared-request-key", "", 0, b, callerHeader, "bob")
		if status != http.StatusUnprocessableEntity || errorCode(t, raw) != api.CodeIdempotencyMismatch {
			t.Fatalf("B's request %q = %d %s, want 422 idempotency_mismatch", b.Message, status, raw)
		}
	}
	requests, err := f.holder.Requests(t.Context(), f.task)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range requests {
		if r.ID == created.ID && (r.Status != created.Status || r.Message != "from A" || r.FromClient != "host-a" || r.Revision != created.Revision) {
			t.Fatalf("A's request changed: %+v, want %+v", r, created)
		}
	}
}

// legacyMove writes a receipt where a server before the receipt scopes kept
// the receipt of key, for a move of the fixture's task to backlog.
func legacyMove(t *testing.T, proj *project, key, id string, move api.Move, r receipt) {
	t.Helper()
	r.Key = key
	if r.Operation == "" {
		r.Operation = "move:" + id
	}
	r.Digest = requestDigest(r.Operation, move)
	if err := writeJSON(proj.root, legacyReceiptPath(key), r); err != nil {
		t.Fatal(err)
	}
}

// B6: a done receipt an older server wrote for the same operation and body
// still answers the retry, and the operation does not run.
func TestReceiptScopeReplaysALegacyReceiptOfTheSameRequest(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	before, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	move := api.Move{State: "backlog", ExpectedRevision: &before.Revision}
	stored, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	legacyMove(t, proj, "legacy-move", id, move, receipt{Status: receiptDone, HTTP: http.StatusOK, Body: stored})
	f := &scopeFixture{http: h, holder: p}
	status, raw := f.post(t, "/tasks/"+id+"/move", "legacy-move", "", 0, move)
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil || status != http.StatusOK || !bytes.Equal(compact.Bytes(), stored) {
		t.Fatalf("retry = %d %s, want the legacy answer %s", status, raw, stored)
	}
	after, err := p.Show(t.Context(), id)
	if err != nil || after.State != "todo" || after.Revision != before.Revision {
		t.Fatalf("the operation ran: %+v %v", after, err)
	}
}

// B7: a pending receipt an older server left for an applied move is finished
// from the card state on retry; the move takes effect once.
func TestReceiptScopeFinishesALegacyPendingReceipt(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	shown, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	move := api.Move{State: "backlog", ExpectedRevision: &shown.Revision}
	legacyMove(t, proj, "legacy-pending", id, move, receipt{Status: receiptPending})
	view, _ := board.LoadBoard(proj.board)
	entry, _ := board.Locate(view, id)
	if _, err := board.MoveWithOptions(entry, proj.board, "backlog", board.MoveOptions{}); err != nil {
		t.Fatal(err)
	}
	applied, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := p.Move(t.Context(), "legacy-pending", id, move, nil)
	if err != nil || moved.State != "backlog" || moved.Revision != applied.Revision {
		t.Fatalf("retry = %+v %v, want the applied move at revision %d", moved, err, applied.Revision)
	}
	var finished receipt
	if found, err := readJSON(proj.root, scopedReceiptPath("move:"+id, "legacy-pending", ""), &finished); err != nil || !found || finished.Status != receiptDone {
		t.Fatalf("the scoped receipt was not finished: %+v %t %v", finished, found, err)
	}
}

// B8: a legacy receipt of another operation under the key may be another
// caller's; it is ignored instead of refusing the request.
func TestReceiptScopeIgnoresALegacyReceiptOfAnotherOperation(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	proj, _ := s.project(p.ID())
	shown, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	move := api.Move{State: "backlog", ExpectedRevision: &shown.Revision}
	legacyMove(t, proj, "legacy-other", id, move, receipt{Operation: "request:" + id, Status: receiptDone, HTTP: http.StatusCreated, Body: json.RawMessage(`{}`)})
	moved, err := p.Move(t.Context(), "legacy-other", id, move, nil)
	if err != nil || moved.State != "backlog" {
		t.Fatalf("move = %+v %v, want it to run", moved, err)
	}
}

// A scoped receipt expires by the same rule as a legacy one: the done
// receipts of an archived card go together once they are older than the age.
func TestServerGCExpiresScopedAndLegacyReceiptsAlike(t *testing.T) {
	f := newGCFixture(t)
	p := f.project("demo")
	task := gcTodoTask(t, p, "alpha", "")
	if _, err := p.Move(t.Context(), "archive", task.TaskID, api.Move{State: "archived", Result: "cancelled", Reason: "test", Decision: "user"}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := f.s.projectByName("demo")
	if err != nil {
		t.Fatal(err)
	}
	proj, err := f.s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	operation := "put:" + task.TaskID + ":notes.md"
	legacy := receipt{Key: "legacy-put", Operation: operation, Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC()}
	scoped := legacy
	scoped.Schema, scoped.Key = receiptSchema, "scoped-put"
	paths := []string{legacyReceiptPath(legacy.Key), scopedReceiptPath(operation, scoped.Key, "")}
	for i, r := range []receipt{legacy, scoped} {
		if err := writeJSON(proj.root, paths[i], r); err != nil {
			t.Fatal(err)
		}
	}
	present := func() (n int) {
		files := receiptFiles(t, f.s, "demo")
		for _, path := range paths {
			if _, ok := files[filepath.Base(path)]; ok {
				n++
			}
		}
		return n
	}
	request := api.ServerGCRequest{OlderThan: "48h", LogKeep: 1 << 20}
	f.clock.advance(47 * time.Hour)
	if _, err := f.gc(request); err != nil || present() != 2 {
		t.Fatalf("47h: %d of 2 receipts left, %v", present(), err)
	}
	f.clock.advance(2 * time.Hour)
	if _, err := f.gc(request); err != nil || present() != 0 {
		t.Fatalf("49h: %d of 2 receipts left, %v", present(), err)
	}
}
