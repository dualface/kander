// Package client is the kander side of /api/v1. It speaks only the typed
// contract of internal/api, retries transport failures with the same
// idempotency key, and never touches a local board.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
)

// Client talks to one server for one client identity.
type Client struct {
	base     *url.URL
	http     *http.Client
	clientID string
	// Retries bounds the attempts of one call after a transport failure or a
	// not-ready answer; each retry reuses the idempotency key.
	Retries int
	Backoff time.Duration
}

// New validates the server URL and the client identity.
func New(serverURL, clientID string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(serverURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("server.url %q must be an http or https URL", serverURL)
	}
	if !api.ValidIdentity(clientID) {
		return nil, fmt.Errorf("client id %q is not a valid identity", clientID)
	}
	return &Client{base: u, http: &http.Client{Timeout: 90 * time.Second}, clientID: clientID, Retries: 3, Backoff: 200 * time.Millisecond}, nil
}

// ClientID returns the identity this client sends.
func (c *Client) ClientID() string { return c.clientID }

// Claim is the context a holder presents on every write of a claimed task.
type Claim struct {
	ReviewWriter  string
	PublicationID string
	Members       map[string]api.ClaimProof
	Token         string
	Generation    uint64
}

// NewKey returns a fresh idempotency key.
func NewKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "k-" + hex.EncodeToString(b)
}

type call struct {
	method string
	path   string
	query  url.Values
	body   any
	key    string
	claim  *Claim
}

func (c *Client) do(ctx context.Context, in call, out any) error {
	var payload []byte
	if in.body != nil {
		var err error
		if payload, err = json.Marshal(in.body); err != nil {
			return err
		}
	}
	if in.method != http.MethodGet && in.key == "" {
		in.key = NewKey()
	}
	u := *c.base
	u.Path = c.base.Path + in.path
	u.RawQuery = in.query.Encode()
	var last error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(ctx.Err(), last)
			case <-time.After(c.Backoff << (attempt - 1)):
			}
		}
		retry, err := c.once(ctx, in, u.String(), payload, out)
		if err == nil || !retry {
			return err
		}
		last = err
	}
	return last
}

// once performs one attempt and reports whether a failure is safe to retry
// with the same key: transport failures and not-ready answers are.
func (c *Client) once(ctx context.Context, in call, target string, payload []byte, out any) (bool, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, in.method, target, body)
	if err != nil {
		return false, err
	}
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderClientID, c.clientID)
	req.Header.Set(api.HeaderOperationID, operationID(in.key))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if in.key != "" {
		req.Header.Set(api.HeaderIdempotencyKey, in.key)
	}
	if in.claim != nil {
		req.Header.Set(api.HeaderReviewWriter, in.claim.ReviewWriter)
		req.Header.Set(api.HeaderPublication, in.claim.PublicationID)
		if len(in.claim.Members) != 0 {
			proofs, err := json.Marshal(in.claim.Members)
			if err != nil {
				return false, err
			}
			req.Header.Set(api.HeaderClaims, string(proofs))
		}
		req.Header.Set(api.HeaderClaimToken, in.claim.Token)
		req.Header.Set(api.HeaderClaimGen, strconv.FormatUint(in.claim.Generation, 10))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ctx.Err() == nil, &TransportError{Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return true, &TransportError{Err: err}
	}
	if resp.StatusCode >= 300 {
		e := &api.Error{Status: resp.StatusCode}
		if json.Unmarshal(data, e) != nil || e.Code == "" {
			e.Code, e.Message = api.CodeInternal, strings.TrimSpace(string(data))
		}
		return e.Code == api.CodeNotReady || resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout, e
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return false, fmt.Errorf("server answer is malformed: %w", err)
		}
	}
	return false, nil
}

func operationID(key string) string {
	if key == "" {
		return NewKey()
	}
	sum := sha256.Sum256([]byte(key))
	return "op-" + hex.EncodeToString(sum[:8])
}

// TransportError is a failure to reach the server or read its answer. The
// effect of a write that ended this way is unknown until a retry with the
// same idempotency key or a read settles it.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return "server unreachable: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// IsUnreachable reports a transport failure.
func IsUnreachable(err error) bool {
	var t *TransportError
	return errors.As(err, &t)
}

// Version reads the server protocol range.
func (c *Client) Version(ctx context.Context) (api.VersionInfo, error) {
	var v api.VersionInfo
	err := c.do(ctx, call{method: http.MethodGet, path: "/api/v1/version"}, &v)
	return v, err
}

// CheckCompatible refuses a server whose protocol range excludes this client.
func (c *Client) CheckCompatible(ctx context.Context) (api.VersionInfo, error) {
	v, err := c.Version(ctx)
	if err != nil {
		return v, err
	}
	if api.ProtocolVersion < v.MinProtocol || v.Protocol < api.MinProtocolVersion {
		return v, &api.Error{Code: api.CodeProtocolIncompatible, Message: fmt.Sprintf("server protocol %d-%d, client protocol %d-%d", v.MinProtocol, v.Protocol, api.MinProtocolVersion, api.ProtocolVersion)}
	}
	return v, nil
}

// Ready reads the readiness probe.
func (c *Client) Ready(ctx context.Context) (api.Health, error) {
	var h api.Health
	err := c.do(ctx, call{method: http.MethodGet, path: "/api/v1/ready"}, &h)
	return h, err
}

// Register binds name (and the Git remote) to a project, creating it once.
func (c *Client) Register(ctx context.Context, name, remote string) (api.Project, error) {
	var p api.Project
	err := c.do(ctx, call{method: http.MethodPost, path: "/api/v1/projects", body: api.RegisterProject{Name: name, Remote: remote}}, &p)
	return p, err
}

// Project returns a Project handle for an already resolved project ID.
func (c *Client) Project(id string) *Project { return &Project{c: c, id: id} }

// Project scopes calls to one project.
type Project struct {
	c  *Client
	id string
}

// ID is the server-assigned project ID.
func (p *Project) ID() string { return p.id }

func (p *Project) path(parts ...string) string {
	escaped := make([]string, 0, len(parts)+2)
	escaped = append(escaped, "/api/v1/projects", url.PathEscape(p.id))
	escaped = append(escaped, parts...)
	return strings.Join(escaped, "/")
}

func taskPath(id string, rest ...string) []string {
	return append([]string{"tasks", url.PathEscape(id)}, rest...)
}

// List reads the task list, optionally limited to one state.
func (p *Project) List(ctx context.Context, state string) (api.TaskList, error) {
	var l api.TaskList
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks"), query: q}, &l)
	return l, err
}

// Show reads one task.
func (p *Project) Show(ctx context.Context, id string) (api.Task, error) {
	var t api.Task
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path(taskPath(id)...)}, &t)
	return t, err
}

// NewTask creates a backlog card.
func (p *Project) NewTask(ctx context.Context, key string, req api.NewTask) (api.Task, error) {
	var t api.Task
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("tasks"), body: req, key: key}, &t)
	return t, err
}

func documentPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// Document reads one card document.
func (p *Project) Document(ctx context.Context, id, path string) (api.Document, error) {
	var d api.Document
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path(taskPath(id, "documents", documentPath(path))...)}, &d)
	return d, err
}

// PutDocument publishes a document under compare-and-swap; claim is required
// while the task is claimed.
func (p *Project) PutDocument(ctx context.Context, key, id string, doc api.Document, claim *Claim) (api.Task, error) {
	sum := sha256.Sum256([]byte(doc.Text))
	doc.SHA256 = hex.EncodeToString(sum[:])
	var t api.Task
	err := p.c.do(ctx, call{method: http.MethodPut, path: p.path(taskPath(id, "documents", documentPath(doc.Path))...), body: doc, key: key, claim: claim}, &t)
	return t, err
}

// Move changes the state of a task outside of claims.
func (p *Project) Move(ctx context.Context, key, id string, req api.Move, claim *Claim) (api.Task, error) {
	var t api.Task
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "move")...), body: req, key: key, claim: claim}, &t)
	return t, err
}

// Claim moves a todo task into working for one execution.
func (p *Project) Claim(ctx context.Context, key, id string, req api.ClaimRequest) (api.ClaimGrant, error) {
	var g api.ClaimGrant
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "claim")...), body: req, key: key}, &g)
	return g, err
}

// Takeover hands a lost claim to a new execution.
func (p *Project) Takeover(ctx context.Context, key, id string, req api.ClaimRequest) (api.ClaimGrant, error) {
	var g api.ClaimGrant
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "takeover")...), body: req, key: key}, &g)
	return g, err
}

// Execution records a launcher fact of the holder's execution.
func (p *Project) Execution(ctx context.Context, key, id string, update api.ExecutionUpdate, claim Claim) (api.Task, error) {
	var t api.Task
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "execution")...), body: update, key: key, claim: &claim}, &t)
	return t, err
}

// Renew extends the lease of claim.
func (p *Project) Renew(ctx context.Context, id string, claim Claim) (api.Renewal, error) {
	var r api.Renewal
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "renew")...), claim: &claim, key: NewKey()}, &r)
	return r, err
}

// Requests lists the collaboration requests of a task.
func (p *Project) Requests(ctx context.Context, id string) ([]api.CollabRequest, error) {
	var out []api.CollabRequest
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path(taskPath(id, "requests")...)}, &out)
	return out, err
}

// CreateRequest asks the holder of a task to act.
func (p *Project) CreateRequest(ctx context.Context, key, id string, req api.NewCollabRequest) (api.CollabRequest, error) {
	if req.FromClient == "" {
		req.FromClient = p.c.clientID
	}
	var r api.CollabRequest
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "requests")...), body: req, key: key}, &r)
	return r, err
}

// AnswerRequest settles a request as its holder.
func (p *Project) AnswerRequest(ctx context.Context, key, id, requestID string, answer api.CollabAnswer, claim Claim) (api.CollabRequest, error) {
	var r api.CollabRequest
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "requests", url.PathEscape(requestID), "answer")...), body: answer, key: key, claim: &claim}, &r)
	return r, err
}

// Events long-polls the change log after cursor for up to wait.
func (p *Project) Events(ctx context.Context, cursor string, wait time.Duration) (api.EventPage, error) {
	var page api.EventPage
	q := url.Values{"cursor": {cursor}, "wait": {strconv.Itoa(int(wait / time.Second))}}
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("events"), query: q}, &page)
	return page, err
}

// ReviewPlan publishes a review plan; body is the board plan JSON.
func (p *Project) ReviewPlan(ctx context.Context, key string, plan any, claim *Claim) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("reviews", "plan"), body: plan, key: key, claim: claim}, &out)
	return out, err
}

// ReviewAggregate publishes the disposition view of a batch.
func (p *Project) ReviewAggregate(ctx context.Context, key, batch string, claim *Claim) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("reviews", "batches", url.PathEscape(batch), "aggregate"), key: key, claim: claim}, &out)
	return out, err
}

// ReviewBatchView reads the published view of a batch.
func (p *Project) ReviewBatchView(ctx context.Context, batch string) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("reviews", "batches", url.PathEscape(batch), "view")}, &out)
	return out, err
}

// ReviewClose closes a batch with the closure request and client Git evidence.
func (p *Project) ReviewClose(ctx context.Context, key string, request any, claim *Claim) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("reviews", "close"), body: request, key: key, claim: claim}, &out)
	return out, err
}

// ReviewProgress reads the review gate progress of a task.
func (p *Project) ReviewProgress(ctx context.Context, task string) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("reviews", "progress", url.PathEscape(task))}, &out)
	return out, err
}

// ImportIssue publishes an issue snapshot through the server's atomic importer.
func (p *Project) ImportIssue(ctx context.Context, key string, request api.IssueImport) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("issues", "import"), body: request, key: key}, &out)
	return out, err
}

// IssueIndex reads the canonical issue-to-card bindings of the server board.
func (p *Project) IssueIndex(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("issues", "index")}, &out)
	return out, err
}

// ReviewAction invokes a controlled evidence producer. The server accepts only
// the declared review actions and decodes each payload into its domain type.
func (p *Project) ReviewAction(ctx context.Context, key, action string, payload any, claim *Claim) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("reviews", "actions", url.PathEscape(action)), body: payload, key: key, claim: claim}, &out)
	return out, err
}

// ReviewResource reads published review metadata, independent of a client CWD.
func (p *Project) ReviewResource(ctx context.Context, kind, id string) (json.RawMessage, error) {
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("reviews", kind, url.PathEscape(id))}, &out)
	return out, err
}

// PreparePublication stores immutable material before holder confirmation.
func (p *Project) PreparePublication(ctx context.Context, request api.PublicationPrepare) (api.Publication, error) {
	var out api.Publication
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("publications"), body: request}, &out)
	return out, err
}

// Publication reads a prepared or completed joint publication.
func (p *Project) Publication(ctx context.Context, id string) (api.Publication, error) {
	var out api.Publication
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("publications", url.PathEscape(id))}, &out)
	return out, err
}

// ConfirmPublication binds this holder's approval to the exact version/digest.
func (p *Project) ConfirmPublication(ctx context.Context, id string, approval api.PublicationApproval, proof Claim) (api.Publication, error) {
	var out api.Publication
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("publications", url.PathEscape(id), "confirm"), body: approval, claim: &proof}, &out)
	return out, err
}
