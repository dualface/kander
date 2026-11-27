package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// logBuffer collects the lines of a server log.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// requests returns the level of each request line by method and path.
func (b *logBuffer) requests(t *testing.T) map[string]string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for scanner.Scan() {
		var line struct{ Level, Msg, Method, Path string }
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		if line.Msg == "request" {
			out[line.Method+" "+line.Path] = line.Level
		}
	}
	return out
}

// logFixture serves one project with a claimed card, logging to log at level.
type logFixture struct {
	s     *Server
	p     *client.Project
	log   *logBuffer
	id    string
	task  string
	proof client.Claim
}

func newLogFixture(t *testing.T, level slog.Level) *logFixture {
	t.Helper()
	f := &logFixture{log: &logBuffer{}}
	s, err := Open(Options{DataDir: t.TempDir(), Version: "test", Logger: slog.New(slog.NewJSONHandler(f.log, &slog.HandlerOptions{Level: level}))})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { h.Close(); s.Close() })
	f.s = s
	c, err := client.New(h.URL, "log-host")
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	f.id, f.p = info.ID, p
	f.task = gcTodoTask(t, p, "alpha", "").TaskID
	grant, err := p.Claim(t.Context(), "", f.task, api.ClaimRequest{BranchMode: "main-develop", ClientID: "log-host", ExecutionID: "exec-a", Owner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	f.proof = client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	return f
}

// call serves one request in process, so its log line is written when call
// returns.
func (f *logFixture) call(t *testing.T, method, path, key string, body any, claim *client.Claim) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	r.Header.Set(api.HeaderClientID, "log-host")
	if key != "" {
		r.Header.Set(api.HeaderIdempotencyKey, key)
	}
	if claim != nil {
		r.Header.Set(api.HeaderClaimToken, claim.Token)
		r.Header.Set(api.HeaderClaimGen, strconv.FormatUint(claim.Generation, 10))
	}
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	if out.Code >= http.StatusBadRequest {
		t.Logf("%s %s: %d %s", method, path, out.Code, out.Body.String())
	}
	return out.Code
}

func (f *logFixture) path(rest string) string { return "/api/v1/projects/" + f.id + rest }

// polls sends one successful request of each polling route of a card.
func (f *logFixture) polls(t *testing.T, wait string) []string {
	t.Helper()
	task := "/tasks/" + f.task
	calls := []struct{ method, path, key string }{
		{http.MethodGet, f.path("/events?wait=" + wait), ""},
		{http.MethodPost, f.path("/snapshot"), ""},
		{http.MethodGet, f.path("/tasks"), ""},
		{http.MethodPost, f.path(task + "/renew"), ""},
		{http.MethodPost, f.path(task + "/observation"), "poll-observe-" + wait},
		{http.MethodGet, f.path(task + "/requests"), ""},
	}
	var lines []string
	for _, c := range calls {
		var body any
		switch {
		case strings.HasSuffix(c.path, "/snapshot"):
			body = api.SnapshotQuery{}
		case strings.HasSuffix(c.path, "/observation"):
			body = api.AgentObservation{Status: "alive"}
		}
		if code := f.call(t, c.method, c.path, c.key, body, &f.proof); code != http.StatusOK {
			t.Fatalf("%s %s = %d", c.method, c.path, code)
		}
		lines = append(lines, c.method+" "+strings.Split(c.path, "?")[0])
	}
	return lines
}

func TestSuccessfulQuickPollingRequestsLogAtDebug(t *testing.T) {
	f := newLogFixture(t, slog.LevelDebug)
	f.log.reset()
	// An event poll that waits a second for nothing is still quick: the wait is
	// what it asked for.
	lines := f.polls(t, "1")
	got := f.log.requests(t)
	for _, line := range lines {
		if got[line] != "DEBUG" {
			t.Fatalf("%s logged at %q, want DEBUG: %v", line, got[line], got)
		}
	}

	// The default level drops them.
	quiet := newLogFixture(t, slog.LevelInfo)
	quiet.log.reset()
	quiet.polls(t, "0")
	if got := quiet.log.requests(t); len(got) != 0 {
		t.Fatalf("the default level must not log successful quick polls: %v", got)
	}
}

func TestFailedSlowAndOtherRequestsKeepTheirLevel(t *testing.T) {
	f := newLogFixture(t, slog.LevelInfo)
	f.log.reset()
	renew := f.path("/tasks/" + f.task + "/renew")
	wrong := client.Claim{Token: strings.Repeat("0", 64), Generation: f.proof.Generation}
	if code := f.call(t, http.MethodPost, renew, "", nil, &wrong); code < http.StatusBadRequest {
		t.Fatalf("a renewal with a wrong token = %d", code)
	}
	if code := f.call(t, http.MethodGet, f.path("/events?wait=x"), "", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("a malformed event poll = %d", code)
	}
	got := f.log.requests(t)
	if got["POST "+renew] != "INFO" || got["GET "+f.path("/events")] != "INFO" {
		t.Fatalf("failed polls must keep INFO: %v", got)
	}

	// Slow polls keep INFO; the threshold is replaced to make every poll slow.
	f.log.reset()
	previous := slowRequest
	slowRequest = 0
	lines := f.polls(t, "0")
	slowRequest = previous
	got = f.log.requests(t)
	for _, line := range lines {
		if got[line] != "INFO" {
			t.Fatalf("slow %s logged at %q, want INFO: %v", line, got[line], got)
		}
	}

	// The observation ticket of an integration is not a polling request.
	f.log.reset()
	ticket := f.path("/integrations/" + api.IntegrationID("refs/heads/main") + "/observation")
	if code := f.call(t, http.MethodGet, ticket, "", nil, nil); code != http.StatusOK {
		t.Fatalf("an integration observation ticket = %d", code)
	}
	if got := f.log.requests(t); got["GET "+ticket] != "INFO" {
		t.Fatalf("a successful integration observation must keep INFO: %v", got)
	}

	// Other requests are logged as before, however quick.
	ctx := t.Context()
	card, err := f.p.Show(ctx, f.task)
	if err != nil {
		t.Fatal(err)
	}
	withdraw := gcTodoTask(t, f.p, "beta", "").TaskID
	open := gcTodoTask(t, f.p, "gamma", "").TaskID
	notes := "notes\n"
	digest := sha256.Sum256([]byte(notes))
	f.log.reset()
	doc := f.path("/tasks/" + f.task + "/documents/notes.md")
	move := f.path("/tasks/" + withdraw + "/move")
	claim := f.path("/tasks/" + open + "/claim")
	for _, c := range []struct {
		method, path, key string
		body              any
		claim             *client.Claim
	}{
		{http.MethodPut, doc, "put-notes", api.Document{Path: "notes.md", Text: notes, SHA256: hex.EncodeToString(digest[:]), ExpectedRevision: card.Revision}, &f.proof},
		{http.MethodPost, move, "move-backlog", api.Move{State: "backlog"}, nil},
		{http.MethodPost, claim, "claim-gamma", api.ClaimRequest{BranchMode: "main-develop", ClientID: "log-host", ExecutionID: "exec-c", Owner: "codex"}, nil},
	} {
		if code := f.call(t, c.method, c.path, c.key, c.body, c.claim); code != http.StatusOK {
			t.Fatalf("%s %s = %d", c.method, c.path, code)
		}
	}
	got = f.log.requests(t)
	for _, line := range []string{"PUT " + doc, "POST " + move, "POST " + claim} {
		if got[line] != "INFO" {
			t.Fatalf("%s logged at %q, want INFO: %v", line, got[line], got)
		}
	}
}

func TestPollingRoutesAreRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range Routes() {
		registered[r.Pattern] = true
	}
	for pattern := range pollingRoutes {
		if !registered[pattern] {
			t.Errorf("polling route %q is not a registered route", pattern)
		}
	}
}

// The wait an event poll asks for, at most 60 seconds, is taken off its
// duration before the threshold applies; nothing else is.
func TestEventWaitIsNotSlowness(t *testing.T) {
	events := func(query string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p/events"+query, nil)
		r.Pattern = projectEvents
		return r
	}
	renew := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/tasks/t/renew", nil)
	renew.Pattern = "POST /api/v1/projects/{project}/tasks/{task}/renew"
	for _, c := range []struct {
		name    string
		r       *http.Request
		status  int
		elapsed time.Duration
		want    slog.Level
	}{
		{"wait=25 took 25.5s", events("?wait=25"), http.StatusOK, 25*time.Second + 500*time.Millisecond, slog.LevelDebug},
		{"wait=25 with a cursor took 25.5s", events("?after=42&wait=25"), http.StatusOK, 25*time.Second + 500*time.Millisecond, slog.LevelDebug},
		{"wait=25 took 26s", events("?wait=25"), http.StatusOK, 26 * time.Second, slog.LevelInfo},
		{"wait=1 took 2.5s", events("?wait=1"), http.StatusOK, 2500 * time.Millisecond, slog.LevelInfo},
		{"wait=1 took 1.999s", events("?wait=1"), http.StatusOK, 1999 * time.Millisecond, slog.LevelDebug},
		{"no wait took 999ms", events(""), http.StatusOK, 999 * time.Millisecond, slog.LevelDebug},
		{"no wait took 1s", events(""), http.StatusOK, time.Second, slog.LevelInfo},
		{"wait=120 counts as 60s, took 60.5s", events("?wait=120"), http.StatusOK, 60*time.Second + 500*time.Millisecond, slog.LevelDebug},
		{"wait=120 counts as 60s, took 61s", events("?wait=120"), http.StatusOK, 61 * time.Second, slog.LevelInfo},
		{"wait=25 failed quickly", events("?wait=25"), http.StatusServiceUnavailable, 10 * time.Millisecond, slog.LevelInfo},
		{"wait=25 refused at 25.5s", events("?wait=25"), http.StatusBadRequest, 25*time.Second + 500*time.Millisecond, slog.LevelInfo},
		{"a renewal has no wait to take off", renew, http.StatusOK, time.Second, slog.LevelInfo},
		{"a renewal took 999ms", renew, http.StatusOK, 999 * time.Millisecond, slog.LevelDebug},
	} {
		if got := requestLevel(c.r, c.status, c.elapsed); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
