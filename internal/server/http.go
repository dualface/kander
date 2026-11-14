package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// maxBody bounds one request body; card documents and reports stay far below.
const maxBody = 32 << 20

// maxWait bounds an event long poll.
const maxWait = 60 * time.Second

// Handler returns the /api/v1 routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.adminRoutes(mux)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, api.Health{Status: "alive", Ready: s.ready.Load()})
	})
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			writeJSONResponse(w, http.StatusServiceUnavailable, api.Health{Status: "starting"})
			return
		}
		writeJSONResponse(w, http.StatusOK, api.Health{Status: "ready", Ready: true})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
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
	const task = "/api/v1/projects/{project}/tasks/{task}"
	s.projectRoute(mux, "GET /api/v1/projects/{project}/tasks", func(p *project, r *http.Request) outcome {
		list, err := p.list(r.Context(), r.URL.Query().Get("state"))
		return result(http.StatusOK, list, err)
	})
	s.projectRoute(mux, "POST /api/v1/projects/{project}/tasks", func(p *project, r *http.Request) outcome {
		var req api.NewTask
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.newTask(r.Header.Get(api.HeaderIdempotencyKey), req)
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
		return p.putDocument(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), doc, holderOf(r))
	})
	s.projectRoute(mux, "POST "+task+"/move", func(p *project, r *http.Request) outcome {
		var req api.Move
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.move(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), req, holderOf(r))
	})
	s.projectRoute(mux, "POST "+task+"/claim", func(p *project, r *http.Request) outcome {
		var req api.ClaimRequest
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.claim(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), req)
	})
	s.projectRoute(mux, "POST "+task+"/takeover", func(p *project, r *http.Request) outcome {
		var req api.ClaimRequest
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.takeover(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), req, holderOf(r))
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
		return p.execution(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), req, holderOf(r))
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
		return p.createRequest(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), req)
	})
	s.projectRoute(mux, "POST "+task+"/requests/{request}/answer", func(p *project, r *http.Request) outcome {
		var req api.CollabAnswer
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.answerRequest(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), r.PathValue("request"), req, holderOf(r))
	})
	s.projectRoute(mux, "GET /api/v1/projects/{project}/events", func(p *project, r *http.Request) outcome {
		wait := time.Duration(0)
		if raw := r.URL.Query().Get("wait"); raw != "" {
			seconds, err := strconv.Atoi(raw)
			if err != nil || seconds < 0 {
				return outcome{err: apiError(api.CodeInvalidRequest, "wait must be a non-negative number of seconds")}
			}
			wait = min(time.Duration(seconds)*time.Second, maxWait)
		}
		// Long polls have no writes to drain and should not delay shutdown.
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(s.shutdown, cancel)
		defer stop()
		defer cancel()
		return outcome{status: http.StatusOK, body: p.events.read(ctx, r.URL.Query().Get("cursor"), wait)}
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
	s.issueRoutes(mux)
	s.issueResultRoutes(mux)
	s.publicationRoutes(mux)
	return mux
}

func result(status int, body any, err error) outcome {
	if err != nil {
		return outcome{err: err}
	}
	return outcome{status: status, body: body}
}

func holderOf(r *http.Request) holder {
	generation, _ := strconv.ParseUint(r.Header.Get(api.HeaderClaimGen), 10, 64)
	h := holder{publication: r.Header.Get(api.HeaderPublication), token: r.Header.Get(api.HeaderClaimToken), generation: generation, clientID: r.Header.Get(api.HeaderClientID)}
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

func (s *Server) projectRoute(mux *http.ServeMux, pattern string, fn func(*project, *http.Request) outcome) {
	mux.HandleFunc(pattern, s.domain(func(r *http.Request) outcome {
		p, err := s.project(r.PathValue("project"))
		if err != nil {
			return outcome{err: err}
		}
		if r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/events") {
			p.mu.Lock()
			defer p.mu.Unlock()
		}
		return fn(p, r)
	}))
}

// domain wraps a domain route with the protocol handshake, the readiness
// gate, the body limit, and the access log. The claim token is never logged.
func (s *Server) domain(fn func(*http.Request) outcome) http.HandlerFunc {
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
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			out = fn(r)
		}
		status := out.status
		if out.err != nil {
			e := asAPIError(out.err)
			if e.Code == api.CodeInternal {
				s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "operation", r.Header.Get(api.HeaderOperationID), "error", out.err)
			}
			status = e.Status
			writeJSONResponse(w, status, e)
		} else {
			writeJSONResponse(w, status, out.body)
		}
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", status,
			"client", r.Header.Get(api.HeaderClientID), "group", r.PathValue("group"), "operation", r.Header.Get(api.HeaderOperationID),
			"generation", r.Header.Get(api.HeaderClaimGen), "duration_ms", time.Since(started).Milliseconds())
	}
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
