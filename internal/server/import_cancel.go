package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/dualface/kander/v2/api"
)

// Reasons of an import that stopped before its publication intent.
const (
	importCancelCaller   = "the client disconnected or canceled the import"
	importCancelShutdown = "the server is shutting down"
)

// importCanceled is a cancellation of the import work that ran before the
// publication intent. Err keeps the original error for the server log.
type importCanceled struct {
	reason string
	err    error
}

func (e *importCanceled) Error() string {
	return "board import canceled: " + e.reason + ": " + e.err.Error()
}

func (e *importCanceled) Unwrap() error { return e.err }

// As lets callers in this process read the cancellation as the API error a
// remote client receives.
func (e *importCanceled) As(target any) bool {
	if out, ok := target.(**api.Error); ok {
		*out = e.apiError()
		return true
	}
	return false
}

// apiError is what a client receives; it never contains a path.
func (e *importCanceled) apiError() *api.Error {
	return apiError(api.CodeImportCanceled, "board import canceled: "+e.reason+"; nothing was published; run the same command again")
}

// classifyImportCancel turns a context error of the import work into an
// importCanceled with its reason. lockWait is the context that bounds only
// the source snapshot lock wait, or nil when there is none. An error that is
// not caused by ctx or lockWait is returned unchanged.
func (s *Server) classifyImportCancel(err error, ctx, lockWait context.Context) error {
	if err == nil || !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return err
	}
	var already *importCanceled
	if errors.As(err, &already) {
		return err
	}
	switch {
	case ctx.Err() == nil && lockWait != nil && errors.Is(lockWait.Err(), context.DeadlineExceeded):
		return &importCanceled{reason: fmt.Sprintf("waiting for the source board lock took longer than %s", s.importLockWait), err: err}
	case ctx.Err() == nil:
		return err
	case s.shutdown != nil && s.shutdown.Err() != nil:
		return &importCanceled{reason: importCancelShutdown, err: err}
	default:
		return &importCanceled{reason: importCancelCaller, err: err}
	}
}

// logImportFailure records the original failure of an import before any
// conversion for the client, which drops unclassified text.
func (s *Server) logImportFailure(project string, err error) {
	var canceled *importCanceled
	switch {
	case errors.As(err, &canceled):
		s.log.Warn("board import canceled", "project_name", project, "reason", canceled.reason, "error", canceled.err)
	case asAPIError(err).Code == api.CodeInternal:
		s.log.Error("board import failed", "project_name", project, "error", err)
	default:
		s.log.Warn("board import refused", "project_name", project, "error", err)
	}
}
