package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/dualface/kander/api"
)

// Hooks are the embedding host's extensions to the request pipeline.
type Hooks struct {
	Authorize   func(context.Context, Request) error
	Attribution func(context.Context) (string, bool)
}

// Request contains the matched route and its path-bound authorization scope.
type Request struct {
	Pattern   string
	Method    string
	Project   string
	ProjectID string
	Task      string
	Group     string
}

func (s *Server) authorize(r *http.Request) *api.Error {
	if s.hooks.Authorize == nil {
		return nil
	}
	req := Request{
		Pattern: r.Pattern, Method: r.Method, ProjectID: matchedValue(r, "project"),
		Task: matchedValue(r, "task"), Group: matchedValue(r, "group"),
	}
	if req.ProjectID != "" {
		// Resolve only registry metadata, without opening or locking a board.
		s.mu.Lock()
		if p := s.projects[req.ProjectID]; p != nil {
			req.Project = p.info.Name
		}
		s.mu.Unlock()
	}
	if err := s.hooks.Authorize(r.Context(), req); err != nil {
		// The policy error may contain private information from the host.
		return apiError(api.CodeForbidden, "request forbidden by the embedding host")
	}
	return nil
}

func matchedValue(r *http.Request, name string) string {
	// A host router can leave values in the request that this route didn't match.
	if strings.Contains(r.Pattern, "/{"+name+"}") {
		return r.PathValue(name)
	}
	return ""
}

type attributionKey struct{}

type callerAttribution struct {
	value string
	found bool
}

func (s *Server) withAttribution(r *http.Request) *http.Request {
	if s.hooks.Attribution == nil {
		return r
	}
	value, found := s.hooks.Attribution(r.Context())
	return r.WithContext(context.WithValue(r.Context(), attributionKey{}, callerAttribution{value: value, found: found}))
}

// attribution selects the host's caller when a hook is configured. An
// explicit absence from the host never falls back to a client claim.
// Attributed writes must call this before touching board state.
func attribution(r *http.Request, clientValue string) (string, error) {
	caller, hooked := r.Context().Value(attributionKey{}).(callerAttribution)
	switch {
	case !hooked:
		return clientValue, nil
	case caller.found:
		return caller.value, nil
	default:
		return "", apiError(api.CodeAttributionRequired, "caller attribution is required")
	}
}
