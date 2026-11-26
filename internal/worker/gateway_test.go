package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	kfs "github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
)

// gatewayMode is what the test gateway does with one request answer.
type gatewayMode int

const (
	// gatewayPass hands the answer to Kander and appends the audit after a 2xx.
	gatewayPass gatewayMode = iota
	// gatewayAuditFails hands the answer to Kander; after a 2xx the audit fails
	// and the worker gets 503 not_ready, as the commercial gateway answers.
	gatewayAuditFails
	// gatewayLostReply hands the answer to Kander and answers 502 without
	// audit: the reply is lost after the commit.
	gatewayLostReply
	// gatewayRefuse answers status and code without reaching Kander.
	gatewayRefuse
	// gatewayHangUp closes the connection without reaching Kander.
	gatewayHangUp
)

type gatewayReply struct {
	mode   gatewayMode
	status int
	code   string
}

// gatewaySend is one request answer as the gateway received it.
type gatewaySend struct {
	request string
	key     string
	body    []byte
	answer  api.CollabAnswer
	at      time.Time
	// status and code are what the worker received.
	status int
	code   string
}

func (s gatewaySend) String() string {
	return fmt.Sprintf("{%s %s rev=%d %q at=%s status=%d}", s.request, s.key, s.answer.ExpectedRevision, s.answer.Note, s.at.Format(time.TimeOnly), s.status)
}

// gatewayTap stands in for an embedder that audits request answers after
// Kander answered them, deduplicated by idempotency key.
type gatewayTap struct {
	now func() time.Time
	// reply chooses the mode of a send; prior counts the earlier sends of the
	// same key. It runs under mu.
	reply func(send gatewaySend, prior int) gatewayReply

	mu     sync.Mutex
	sends  []gatewaySend
	audits map[string]int
}

func newGateway(reply func(gatewaySend, int) gatewayReply) *gatewayTap {
	return &gatewayTap{reply: reply, audits: map[string]int{}}
}

// harness starts a harness behind the gateway, which reads its clock.
func (g *gatewayTap) harness(t *testing.T) *harness {
	h := newHarnessWrapped(t, 0, g.wrap)
	g.now = h.now
	return h
}

func (g *gatewayTap) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/answer") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		var answer api.CollabAnswer
		if err != nil || json.Unmarshal(body, &answer) != nil {
			http.Error(w, "unreadable test answer", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		segments := strings.Split(r.URL.Path, "/")
		send := gatewaySend{request: segments[len(segments)-2], key: r.Header.Get(api.HeaderIdempotencyKey), body: body, answer: answer}
		if g.now != nil {
			send.at = g.now()
		}
		g.mu.Lock()
		prior := 0
		for _, earlier := range g.sends {
			if earlier.key == send.key {
				prior++
			}
		}
		var reply gatewayReply
		if g.reply != nil {
			reply = g.reply(send, prior)
		}
		index := len(g.sends)
		g.sends = append(g.sends, send)
		g.mu.Unlock()
		status, code := g.serve(w, r, next, send.key, reply)
		g.mu.Lock()
		g.sends[index].status, g.sends[index].code = status, code
		g.mu.Unlock()
	})
}

func (g *gatewayTap) serve(w http.ResponseWriter, r *http.Request, next http.Handler, key string, reply gatewayReply) (int, string) {
	switch reply.mode {
	case gatewayRefuse:
		denyTestAnswer(w, reply.status, reply.code)
		return reply.status, reply.code
	case gatewayHangUp:
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return 0, ""
	}
	recorded := httptest.NewRecorder()
	next.ServeHTTP(recorded, r)
	committed := recorded.Code < 300
	switch {
	case reply.mode == gatewayLostReply:
		denyTestAnswer(w, http.StatusBadGateway, api.CodeInternal)
		return http.StatusBadGateway, api.CodeInternal
	case committed && reply.mode == gatewayAuditFails:
		denyTestAnswer(w, http.StatusServiceUnavailable, api.CodeNotReady)
		return http.StatusServiceUnavailable, api.CodeNotReady
	case committed:
		g.mu.Lock()
		if g.audits[key] == 0 {
			g.audits[key] = 1
		}
		g.mu.Unlock()
	}
	for name, values := range recorded.Header() {
		w.Header()[name] = values
	}
	w.WriteHeader(recorded.Code)
	_, _ = w.Write(recorded.Body.Bytes())
	var refusal api.Error
	if !committed {
		_ = json.Unmarshal(recorded.Body.Bytes(), &refusal)
	}
	return recorded.Code, refusal.Code
}

// sendsOf returns the sends of the request whose answer has the note.
func (g *gatewayTap) sendsOf(request, note string) []gatewaySend {
	g.mu.Lock()
	defer g.mu.Unlock()
	var found []gatewaySend
	for _, send := range g.sends {
		if send.request == request && send.answer.Note == note {
			found = append(found, send)
		}
	}
	return found
}

// ofRequest returns every send of the request.
func (g *gatewayTap) ofRequest(request string) []gatewaySend {
	g.mu.Lock()
	defer g.mu.Unlock()
	var found []gatewaySend
	for _, send := range g.sends {
		if send.request == request {
			found = append(found, send)
		}
	}
	return found
}

// withKey returns the sends of a key.
func (g *gatewayTap) withKey(key string) []gatewaySend {
	g.mu.Lock()
	defer g.mu.Unlock()
	var found []gatewaySend
	for _, send := range g.sends {
		if send.key == key {
			found = append(found, send)
		}
	}
	return found
}

func (g *gatewayTap) all() []gatewaySend {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gatewaySend(nil), g.sends...)
}

func (g *gatewayTap) audited(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.audits[key]
}

// requireStableSends checks the replay invariant in both directions: every
// send of one key carries the same body, byte for byte, and every send of one
// managed answer (one request and body) carries the same key.
func (g *gatewayTap) requireStableSends(t *testing.T) {
	t.Helper()
	checker := stableSends{}
	for _, send := range g.all() {
		checker.add(t, send.request, send.key, send.body, send.answer)
	}
}

// stableSends holds the first body of each key and the first key of each
// managed answer for requireStableSends.
type stableSends struct {
	bodies map[string][]byte
	keys   map[string]string
}

func (s *stableSends) add(t *testing.T, request, key string, body []byte, answer api.CollabAnswer) {
	t.Helper()
	if s.bodies == nil {
		s.bodies, s.keys = map[string][]byte{}, map[string]string{}
	}
	if first, seen := s.bodies[key]; seen && !bytes.Equal(first, body) {
		t.Fatalf("key %s was sent with two bodies: %s and %s", key, first, body)
	} else if !seen {
		s.bodies[key] = body
	}
	if !managedNote(answer.Note) {
		return
	}
	answerID := request + "\x00" + string(body)
	if first, seen := s.keys[answerID]; seen && first != key {
		t.Fatalf("answer %s of request %s was sent with two keys: %s and %s", body, request, first, key)
	} else if !seen {
		s.keys[answerID] = key
	}
}

// managedNote reports whether a note is one of the managed answers; the
// reservation and its confirmation use other keys by design.
func managedNote(note string) bool {
	switch note {
	case typedNote, ackedNote, unconfirmedNote, staleNote:
		return true
	}
	return strings.HasPrefix(note, expiredNote)
}

// requireOneAttempt checks that every send of one logical answer attempt,
// chosen by the test, carries the key and the raw body of its first send: a
// replay that rebuilt both would not pass.
func requireOneAttempt(t *testing.T, what string, sends []gatewaySend) {
	t.Helper()
	if len(sends) == 0 {
		t.Fatalf("%s: no send", what)
	}
	for _, send := range sends[1:] {
		if send.key != sends[0].key || !bytes.Equal(send.body, sends[0].body) {
			t.Fatalf("%s: a send differs from the first: %v then %v", what, sends[0], send)
		}
	}
}

// requireContentKeys checks that every typed note and acknowledgement result
// the worker sent for the request carries the content key of its body.
func (g *gatewayTap) requireContentKeys(t *testing.T, c remote.Credential, request string) {
	t.Helper()
	for _, send := range g.all() {
		switch send.answer.Note {
		case typedNote, ackedNote, unconfirmedNote:
			if want := contentKey(c.ExecutionID, request, send.answer); send.key != want {
				t.Fatalf("answer %+v was sent with key %s, want the content key %s", send.answer, send.key, want)
			}
		}
	}
}

// countRequestFiles counts the request files the harness worker creates.
func (h *harness) countRequestFiles() *atomic.Int32 {
	var n atomic.Int32
	create := h.loop.o.CreateRequestFile
	h.loop.o.CreateRequestFile = func(body string) (*kfs.TempDir, string, error) {
		n.Add(1)
		return create(body)
	}
	return &n
}

// debugLogs routes the log of the harness worker, DEBUG included, into a sink.
func (h *harness) debugLogs() *logSink {
	sink := &logSink{}
	h.loop.o.Logger = slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return sink
}

// openAttempts returns the open attempts of the credential.
func (h *harness) openAttempts(c remote.Credential) []*answerAttempt {
	if table := h.loop.replays[keyOf(c)]; table != nil {
		return table.attempts
	}
	return nil
}
