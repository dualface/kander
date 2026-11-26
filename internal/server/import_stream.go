package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
)

// importWriteTimeout bounds one write to a streaming client, so a client that
// stops reading cannot hold the handler after the import ended.
const importWriteTimeout = 30 * time.Second

// importRoute serves POST /api/v1/server/import. A client that accepts
// application/x-ndjson receives the progress of the import and then its
// result; any other client receives the single JSON of the result.
func (s *Server) importRoute(r *http.Request) outcome {
	if !s.administrator(r) {
		return outcome{err: &api.Error{Code: api.CodeInvalidRequest, Message: "server import refused", Status: http.StatusForbidden}}
	}
	var req api.ImportRequest
	if err := decode(r, &req); err != nil {
		return outcome{err: err}
	}
	if !filepath.IsAbs(req.From) {
		return outcome{err: apiError(api.CodeInvalidRequest, "import source must be an absolute path on the server host")}
	}
	ctx, cancel := context.WithTimeout(r.Context(), ImportTimeout)
	if !strings.Contains(r.Header.Get("Accept"), api.ImportStreamType) {
		defer cancel()
		value, err := s.importV1Board(ctx, req, nil)
		return result(http.StatusOK, value, err)
	}
	return outcome{stream: func(w http.ResponseWriter) int {
		defer cancel()
		return s.streamImport(w, r, func(report func(api.ImportProgress)) (api.ImportResult, error) {
			return s.importV1Board(ctx, req, report)
		})
	}}
}

// streamImport answers 200 at once and writes one JSON line per progress
// report, then the result or the error. Progress is coalesced: the importing
// goroutine holds the project lock while it reports, so it must never wait for
// a slow client, and only the latest report is kept for the writer.
func (s *Server) streamImport(w http.ResponseWriter, r *http.Request, run func(func(api.ImportProgress)) (api.ImportResult, error)) int {
	w.Header().Set("Content-Type", api.ImportStreamType)
	w.WriteHeader(http.StatusOK)
	out := &importStream{w: w, rc: http.NewResponseController(w)}
	latest := newLatestProgress()
	stopped := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-latest.wake:
				out.progress(latest.take())
			case <-finished:
				out.progress(latest.take())
				return
			}
		}
	}()
	value, err := run(latest.put)
	close(finished)
	<-stopped
	if err != nil {
		e := s.failure(r, err)
		out.write(api.ImportStreamLine{Status: e.Status, Error: e})
		return e.Status
	}
	out.write(api.ImportStreamLine{Result: &value})
	return http.StatusOK
}

// importStream writes JSON lines and flushes each. A failed write is dropped:
// the import keeps running, and the request context decides cancellation.
type importStream struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (o *importStream) progress(queue []api.ImportProgress) {
	for i := range queue {
		o.write(api.ImportStreamLine{Progress: &queue[i]})
	}
}

func (o *importStream) write(line api.ImportStreamLine) {
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	_ = o.rc.SetWriteDeadline(time.Now().Add(importWriteTimeout))
	if _, err := o.w.Write(append(data, '\n')); err == nil {
		_ = o.rc.Flush()
	}
}

// latestProgress queues the reports not yet written and wakes its one reader.
// A report of the phase the queue ends with replaces it, so counts coalesce
// while every phase change is kept, and the queue holds at most one entry per
// phase.
type latestProgress struct {
	mu    sync.Mutex
	queue []api.ImportProgress
	wake  chan struct{}
}

func newLatestProgress() *latestProgress { return &latestProgress{wake: make(chan struct{}, 1)} }

func (l *latestProgress) put(p api.ImportProgress) {
	l.mu.Lock()
	if n := len(l.queue); n > 0 && l.queue[n-1].Phase == p.Phase {
		l.queue[n-1] = p
	} else {
		l.queue = append(l.queue, p)
	}
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (l *latestProgress) take() []api.ImportProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	queue := l.queue
	l.queue = nil
	return queue
}
