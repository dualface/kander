package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/i18n"
)

// maxBody bounds one request body; card documents and reports stay far below.
const maxBody = 32 << 20

// maxWait bounds an event long poll.
const maxWait = 60 * time.Second

// Handler returns the /api/v1 routes.
func (s *Server) Handler() http.Handler {
	return s.routes().handler()
}

type route struct {
	pattern    string
	handler    http.HandlerFunc
	authorized bool
}

// routeTable is the inventory used to mount and verify the same set of routes.
type routeTable []route

func (t *routeTable) HandleFunc(pattern string, handler http.HandlerFunc) {
	*t = append(*t, route{pattern: pattern, handler: handler, authorized: true})
}

func (t *routeTable) probe(pattern string, handler http.HandlerFunc) {
	*t = append(*t, route{pattern: pattern, handler: handler})
}

func (t routeTable) handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range t {
		mux.HandleFunc(route.pattern, route.handler)
	}
	return mux
}

func (s *Server) routes() routeTable {
	var table routeTable
	s.registerRoutes(&table)
	return table
}

// Route describes the method and path pattern and whether it uses authorization.
type Route struct {
	Pattern    string
	Authorized bool
}

// Routes returns a fresh inventory from the same registrations as Handler.
func Routes() []Route {
	table := (&Server{}).routes()
	routes := make([]Route, 0, len(table))
	for _, r := range table {
		routes = append(routes, Route{Pattern: r.pattern, Authorized: r.authorized})
	}
	return routes
}

func (s *Server) registerRoutes(mux *routeTable) {
	s.adminRoutes(mux)
	mux.HandleFunc("POST "+api.ImportUploadRoute, s.domainWithBodyLimit(s.upload.wireBytes(), s.importUploadRoute))
	mux.probe("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, api.Health{Status: "alive", Ready: s.ready.Load()})
	})
	mux.probe("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			writeJSONResponse(w, http.StatusServiceUnavailable, api.Health{Status: "starting"})
			return
		}
		writeJSONResponse(w, http.StatusOK, api.Health{Status: "ready", Ready: true})
	})
	mux.probe("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: s.version})
	})
	mux.HandleFunc("GET /api/v1/projects", s.domain(func(r *http.Request) outcome {
		info, err := s.projectByName(r.URL.Query().Get("name"))
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: info}
	}))
	mux.HandleFunc("POST /api/v1/projects", s.domain(func(r *http.Request) outcome {
		var req api.RegisterProject
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		info, created, err := s.register(req)
		if err != nil {
			return outcome{err: err}
		}
		if created {
			return outcome{status: http.StatusCreated, body: info}
		}
		return outcome{status: http.StatusOK, body: info}
	}))
	// The project by its ID, for a client that holds the ID but not the name,
	// such as kander gc matching a client project directory to its project.
	s.projectRoute(mux, "GET /api/v1/projects/{project}", func(p *project, _ *http.Request) outcome {
		return outcome{status: http.StatusOK, body: p.view()}
	})
	s.policyRoutes(mux)
	const task = "/api/v1/projects/{project}/tasks/{task}"
	s.projectRoute(mux, taskListRoute, func(p *project, r *http.Request) outcome {
		query, err := parseTaskList(r.URL.RawQuery)
		if err != nil {
			return outcome{err: err}
		}
		list, err := p.list(r.Context(), query)
		return result(http.StatusOK, list, err)
	})
	s.projectRoute(mux, "POST /api/v1/projects/{project}/tasks", func(p *project, r *http.Request) outcome {
		req, err := decodeNewTask(r)
		if err != nil {
			return outcome{err: err}
		}
		submitter, err := attribution(r, req.Submitter)
		if err != nil {
			return outcome{err: err}
		}
		return p.newTask(scopeOf(r), req, submitter)
	})
	s.projectRoute(mux, "GET "+task, func(p *project, r *http.Request) outcome {
		t, err := p.show(r.PathValue("task"))
		return result(http.StatusOK, t, err)
	})
	s.projectRoute(mux, "GET "+task+"/documents/{path...}", func(p *project, r *http.Request) outcome {
		d, err := p.document(r.PathValue("task"), r.PathValue("path"))
		return result(http.StatusOK, d, err)
	})
	s.projectRoute(mux, "PUT "+task+"/documents/{path...}", func(p *project, r *http.Request) outcome {
		var doc api.Document
		if err := decode(r, &doc); err != nil {
			return outcome{err: err}
		}
		if doc.Path != r.PathValue("path") {
			return outcome{err: apiError(api.CodeInvalidRequest, "document path does not match the URL")}
		}
		return p.putDocument(scopeOf(r), r.PathValue("task"), doc, holderOf(r))
	})
	s.projectRoute(mux, "POST "+task+"/move", func(p *project, r *http.Request) outcome {
		var req api.Move
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.move(scopeOf(r), r.PathValue("task"), req, holderOf(r))
	})
	s.projectRoute(mux, "POST "+task+"/confirm", confirmRequest)
	s.projectRoute(mux, "POST "+task+"/assignee", func(p *project, r *http.Request) outcome {
		var req api.Assign
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		if err := s.authorizeAssignment(r, req.Assignee); err != nil {
			return outcome{err: err}
		}
		return p.assign(scopeOf(r), r.PathValue("task"), req)
	})
	s.registerDecisionRoutes(mux, task)
	s.projectRoute(mux, "POST "+task+"/claim", func(p *project, r *http.Request) outcome {
		var req api.ClaimRequest
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.claim(scopeOf(r), r.PathValue("task"), req)
	})
	s.projectRoute(mux, "POST "+task+"/takeover", func(p *project, r *http.Request) outcome {
		var req api.ClaimRequest
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.takeover(scopeOf(r), r.PathValue("task"), req, holderOf(r))
	})
	s.projectRoute(mux, "GET "+task+"/execution/record", func(p *project, r *http.Request) outcome {
		record, _, err := board.CurrentExecution(p.board, r.PathValue("task"))
		return result(http.StatusOK, record, err)
	})
	s.projectRoute(mux, "GET "+task+"/execution", func(p *project, r *http.Request) outcome {
		value, err := p.executionState(r.PathValue("task"), holderOf(r))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+task+"/execution", func(p *project, r *http.Request) outcome {
		var req api.ExecutionUpdate
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.execution(scopeOf(r), r.PathValue("task"), req, holderOf(r))
	})
	s.projectRoute(mux, "POST "+task+"/renew", func(p *project, r *http.Request) outcome {
		renewal, err := p.renew(r.PathValue("task"), holderOf(r))
		return result(http.StatusOK, renewal, err)
	})
	s.projectRoute(mux, "GET "+task+"/requests", func(p *project, r *http.Request) outcome {
		list, err := p.requests(r.PathValue("task"))
		return result(http.StatusOK, list, err)
	})
	s.projectRoute(mux, "POST "+task+"/requests", func(p *project, r *http.Request) outcome {
		var req api.NewCollabRequest
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.createRequest(scopeOf(r), r.PathValue("task"), req)
	})
	s.projectRoute(mux, "POST "+task+"/requests/{request}/answer", func(p *project, r *http.Request) outcome {
		var req api.CollabAnswer
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.answerRequest(scopeOf(r), r.PathValue("task"), r.PathValue("request"), req, holderOf(r))
	})
	s.projectRoute(mux, projectEvents, func(p *project, r *http.Request) outcome {
		wait, err := eventWait(r)
		if err != nil {
			return outcome{err: err}
		}
		// Long polls have no writes to drain and should not delay shutdown.
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(s.shutdown, cancel)
		defer stop()
		defer cancel()
		page := p.events.read(ctx, r.URL.Query().Get("cursor"), wait)
		// Read after the events: a policy write publishes the policy before its
		// event, so the version is never older than an event of the page.
		policy := p.policyView()
		page.PolicyEpoch, page.PolicyRevision = policy.Epoch, policy.Revision
		return outcome{status: http.StatusOK, body: page}
	})
	s.reviewRoutes(mux)
	s.reviewActionRoutes(mux)
	s.reviewRuntimeRoutes(mux)
	s.observationRoutes(mux)
	s.snapshotRoutes(mux)
	s.groupRoutes(mux)
	s.dispatchRoutes(mux)
	s.deliveryRoutes(mux)
	s.integrationRoutes(mux)
	s.taskIntegrationRoutes(mux)
	s.evidenceCleanupRoutes(mux)
	s.issueRoutes(mux)
	s.issueResultRoutes(mux)
	s.publicationRoutes(mux)
	s.hostRoutes(mux)
	s.startOrderRoutes(mux)
}

func result(status int, body any, err error) outcome {
	if err != nil {
		return outcome{err: err}
	}
	return outcome{status: status, body: body}
}

func holderOf(r *http.Request) holder {
	generation, _ := strconv.ParseUint(r.Header.Get(api.HeaderClaimGen), 10, 64)
	h := holder{publication: r.Header.Get(api.HeaderPublication), token: r.Header.Get(api.HeaderClaimToken), generation: generation, clientID: r.Header.Get(api.HeaderClientID), basis: coordinatorBasisOf(r)}
	if raw := r.Header.Get(api.HeaderClaims); raw != "" {
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &h.members) != nil || len(h.members) == 0 || len(h.members) > 256 {
			h.invalid = apiError(api.CodeInvalidRequest, "invalid multi-task claim proofs")
		}
		for id, proof := range h.members {
			if !api.ValidIdentity(id) || len(proof.Token) != 64 || proof.Generation == 0 {
				h.invalid = apiError(api.CodeInvalidRequest, "invalid multi-task claim proof")
			}
		}
	}
	return h
}

// projectEvents is the one read route that never touches the board.
const projectEvents = "GET /api/v1/projects/{project}/events"

func (s *Server) projectRoute(mux *routeTable, pattern string, fn func(*project, *http.Request) outcome) {
	// The registered pattern decides the lock: a GET pattern also serves
	// HEAD, and a request path ending in /events may still name a document.
	read := strings.HasPrefix(pattern, http.MethodGet+" ") && pattern != projectEvents && pattern != taskListRoute
	mux.HandleFunc(pattern, s.domain(func(r *http.Request) outcome {
		p, err := s.project(r.PathValue("project"))
		if err != nil {
			return outcome{err: err}
		}
		if read {
			if err := p.lockBoard(); err != nil {
				return outcome{err: err}
			}
			defer p.unlockBoard()
		}
		return fn(p, r)
	}))
}

// domain wraps a domain route with the protocol handshake, the readiness
// gate, embedding hooks, the body limit, and the access log.
func (s *Server) domain(fn func(*http.Request) outcome) http.HandlerFunc {
	return s.domainWithBodyLimit(maxBody, fn)
}

func (s *Server) domainWithBodyLimit(bodyLimit int64, fn func(*http.Request) outcome) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
		var out outcome
		switch peer, err := strconv.Atoi(r.Header.Get(api.HeaderProtocol)); {
		case err != nil || peer < api.MinProtocolVersion || peer > api.ProtocolVersion:
			out = outcome{err: apiError(api.CodeProtocolIncompatible, "client protocol "+r.Header.Get(api.HeaderProtocol)+" is not supported; server speaks "+strconv.Itoa(api.ProtocolVersion))}
		case !s.ready.Load():
			out = outcome{err: apiError(api.CodeNotReady, "server is recovering")}
		default:
			if scope, err := s.authorize(r); err != nil {
				out = outcome{err: err}
			} else {
				r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
				request := s.withAttribution(r)
				if s.hooks.AuthorizeAssignment != nil && s.hooks.Authorize != nil {
					// Keep the same scope even if the registry changes between hooks.
					request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
				}
				out = fn(request)
			}
		}
		status := out.status
		switch {
		case out.stream != nil:
			status = out.stream(w)
		case out.err != nil:
			e := s.failure(r, out.err)
			status = e.Status
			writeJSONResponse(w, status, e)
		default:
			writeJSONResponse(w, status, out.body)
		}
		elapsed := time.Since(started)
		s.log.Log(r.Context(), requestLevel(r, status, elapsed), "request", "method", r.Method, "path", r.URL.Path, "status", status,
			"client", r.Header.Get(api.HeaderClientID), "group", r.PathValue("group"), "operation", r.Header.Get(api.HeaderOperationID),
			"generation", r.Header.Get(api.HeaderClaimGen), "duration_ms", elapsed.Milliseconds())
	}
}

// pollingRoutes are the routes clients call on a timer: the worker's renewals,
// observations and request lists, and the subscribers' and the TUI's event
// polls, snapshots and task lists. The observation ticket of an integration is
// not one: a client takes it when it prepares or submits an integration step.
var pollingRoutes = map[string]bool{
	projectEvents: true,
	taskListRoute: true,
	"POST /api/v1/projects/{project}/snapshot":                         true,
	"POST /api/v1/projects/{project}/tasks/{task}/renew":               true,
	"POST /api/v1/projects/{project}/tasks/{task}/observation":         true,
	"GET /api/v1/projects/{project}/tasks/{task}/requests":             true,
	"POST /api/v1/projects/{project}/groups/{group}/renew":             true,
	"POST /api/v1/projects/{project}/groups/{group}/observation":       true,
	"POST /api/v1/projects/{project}/integrations/{integration}/renew": true,
	"POST " + api.HostHeartbeatRoute:                                   true,
}

// slowRequest is the duration from which a polling request is logged like any
// other request; tests replace it.
var slowRequest = time.Second

// requestLevel is the level of the request log line: DEBUG for a polling
// request that succeeded in less than slowRequest, INFO otherwise. The time an
// event poll asked to wait, at most maxWait, is not slowness and does not count.
func requestLevel(r *http.Request, status int, elapsed time.Duration) slog.Level {
	if !pollingRoutes[r.Pattern] || status >= http.StatusBadRequest {
		return slog.LevelInfo
	}
	if r.Pattern == projectEvents || r.Pattern == "POST "+api.HostHeartbeatRoute {
		// The route refused a malformed wait, so the status is already 400.
		wait, _ := eventWait(r)
		elapsed -= wait
	}
	if elapsed >= slowRequest {
		return slog.LevelInfo
	}
	return slog.LevelDebug
}

// eventWait is the wait an event poll asks for, bounded by maxWait; no value
// waits for nothing.
func eventWait(r *http.Request) (time.Duration, error) {
	raw := r.URL.Query().Get("wait")
	if raw == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 0, apiError(api.CodeInvalidRequest, "wait must be a non-negative number of seconds")
	}
	return min(time.Duration(seconds)*time.Second, maxWait), nil
}

// failure converts an operation error to its API error and logs an internal one.
func (s *Server) failure(r *http.Request, err error) *api.Error {
	e := asAPIError(err)
	if e.Code == api.CodeInternal {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "operation", r.Header.Get(api.HeaderOperationID), "error", err)
	}
	return e
}

func decodeNewTask(r *http.Request) (api.NewTask, error) {
	// Decode the description separately so JSON's replacement of invalid UTF-8
	// cannot silently change the submitter's text. Other fields decode as before.
	var wire struct {
		api.NewTask
		Description json.RawMessage `json:"description"`
	}
	if err := decode(r, &wire); err != nil {
		return api.NewTask{}, err
	}
	if len(wire.Description) != 0 {
		if !utf8.Valid(wire.Description) {
			reason := i18n.Text("en", "board.request_description_utf8")
			return api.NewTask{}, apiError(api.CodeInvalidRequest, i18n.Text("en", "board.request_description_invalid", reason))
		}
		if err := json.Unmarshal(wire.Description, &wire.NewTask.Description); err != nil {
			return api.NewTask{}, apiError(api.CodeInvalidRequest, "malformed request body")
		}
	}
	return wire.NewTask, nil
}

func decode(r *http.Request, value any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apiError(api.CodeInvalidRequest, "request body is too large")
		}
		return apiError(api.CodeInvalidRequest, "malformed request body")
	}
	if _, err := dec.Token(); err != io.EOF {
		return apiError(api.CodeInvalidRequest, "trailing data after the request body")
	}
	return nil
}

func writeJSONResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if raw, ok := body.(json.RawMessage); ok {
		w.Write(raw)
		w.Write([]byte("\n"))
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}
