// Package server embeds the Kander project service in a host process.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/dualface/kander/api"
	core "github.com/dualface/kander/internal/server"
)

// Options configure an embedded instance. DataDir must be an absolute path.
// Zero lease durations and a nil Logger use the service defaults.
type Options struct {
	// DataDir is the absolute path of the host's Kander data directory.
	DataDir string
	// TTL is the duration of a claim lease.
	TTL time.Duration
	// RenewInterval is the recommended interval between lease renewals.
	RenewInterval time.Duration
	// Logger receives server diagnostics.
	Logger *slog.Logger
	// Version is the build identifier reported by the service.
	Version string
	// Hooks supply optional host authorization and attribution.
	Hooks Hooks
}

// Hooks provide the host's request policy and caller identity.
// Requests can invoke the hooks concurrently.
type Hooks struct {
	// Authorize is called once for an eligible request before any board access.
	// A nil hook allows the request. An error denies it without exposing its text.
	Authorize func(context.Context, Request) error
	// Attribution takes the accepted request's context and reports the host's
	// caller identity. When set, a false result denies attributed operations
	// instead of falling back to a caller-supplied value.
	Attribution func(context.Context) (string, bool)
}

// Request describes the matched route and its authorization scope. Pattern
// is the registered method and path template, while Method is the actual HTTP
// method. Project is the registered name for ProjectID, when known. ProjectID,
// Task and Group come only from the matching URL path parameters.
type Request struct {
	// Pattern is the registered method and path template.
	Pattern string
	// Method is the incoming HTTP method.
	Method string
	// Project is the registered name, or empty when not known or scoped.
	Project string
	// ProjectID is the matched project ID, or empty on a host-level route.
	ProjectID string
	// Task is the matched task ID, or empty when not scoped to a task.
	Task string
	// Group is the matched group ID, or empty when not scoped to a group.
	Group string
}

// Notification contains a project event or a reset of the subscriber's view.
// Cursor is the position of Event or, on Reset, the current position of the log.
type Notification struct {
	// Event is the recorded event. It is zero on reset.
	Event api.Event
	// Cursor is the event position or the stream tip at reset.
	Cursor string
	// Reset requires the subscriber to refresh its view of the board.
	Reset bool
}

// Server is an opened Kander service with exclusive use of its data directory.
type Server struct {
	impl *core.Server
}

// Open locks the data directory and recovers its projects. It does not listen
// on a socket, register signal handlers, or manage the host process.
func Open(options Options) (*Server, error) {
	hooks := core.Hooks{Attribution: options.Hooks.Attribution}
	if authorize := options.Hooks.Authorize; authorize != nil {
		hooks.Authorize = func(ctx context.Context, r core.Request) error {
			return authorize(ctx, Request(r))
		}
	}
	s, err := core.Open(core.Options{
		DataDir: options.DataDir, TTL: options.TTL, RenewInterval: options.RenewInterval,
		Logger: options.Logger, Version: options.Version, Hooks: hooks,
	})
	if err != nil {
		return nil, err
	}
	return &Server{impl: s}, nil
}

// Handler returns the service's /api/v1 routes. Wrap it in the host's
// authentication middleware and mount it at the root of its URL space.
func (s *Server) Handler() http.Handler { return s.impl.Handler() }

// TaskAccess returns current attribution, state and revision for a task in a
// project identified by its ID. It does not invoke authorization hooks and can
// be called from one. A missing resource returns api.CodeNotFound, and an
// untrusted index returns api.CodeIndexRebuilding. Hosts should deny on errors.
func (s *Server) TaskAccess(ctx context.Context, projectID, taskID string) (api.TaskAccess, error) {
	return s.impl.TaskAccess(ctx, projectID, taskID)
}

// Subscribe streams events for a registered project by ID, after the given
// /events or snapshot cursor. A stale or empty cursor starts with a reset.
// A slow reader can also receive a reset and should refresh its board view.
// The channel closes on context cancellation or service shutdown.
func (s *Server) Subscribe(ctx context.Context, projectID, cursor string) (<-chan Notification, error) {
	return core.Subscribe(ctx, s.impl, projectID, cursor, func(event api.Event, cursor string, reset bool) Notification {
		return Notification{Event: event, Cursor: cursor, Reset: reset}
	})
}

// Close releases the data directory lock and cancels subscriptions. The host
// must stop serving and drain active requests before calling Close.
func (s *Server) Close() error { return s.impl.Close() }
