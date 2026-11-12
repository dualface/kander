package server

import (
	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"net/http"
)

func (s *Server) snapshotRoutes(mux *http.ServeMux) {
	s.projectRoute(mux, "POST /api/v1/projects/{project}/check", func(p *project, r *http.Request) outcome {
		var query api.CheckQuery
		if err := decode(r, &query); err != nil {
			return outcome{err: err}
		}
		if len(query.Tasks) > 10000 {
			return outcome{err: apiError(api.CodeInvalidRequest, "check selection exceeds its bound")}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		code, output, diagnostics, err := board.CheckBoard(p.board, query.Tasks, query.All)
		return result(http.StatusOK, api.CheckResult{Code: code, Output: output, Diagnostics: diagnostics}, err)
	})

	s.projectRoute(mux, "POST /api/v1/projects/{project}/snapshot", func(p *project, r *http.Request) outcome {
		var query api.SnapshotQuery
		if err := decode(r, &query); err != nil {
			return outcome{err: err}
		}
		if len(query.Tasks) > 10000 || len(query.Groups) > 1000 {
			return outcome{err: apiError(api.CodeInvalidRequest, "snapshot selection exceeds its bound")}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		scanned, err := board.ScanDispatchesContext(r.Context(), p.board, query.Tasks)
		if err != nil {
			return outcome{err: err}
		}
		view, err := board.CaptureView(scanned, "project:"+p.info.ID)
		if err != nil {
			return outcome{err: err}
		}
		out := api.BoardSnapshot{ObservedAt: p.server.now(), View: view, Claims: map[string]*api.ClaimView{}, Blocked: map[string]string{}, Cursor: p.events.cursor()}
		for id := range scanned.Entries {
			claim, found, err := p.readClaim(id)
			if err != nil {
				return outcome{err: err}
			}
			if found {
				out.Claims[id] = claim.view(p.server.now())
			}
		}
		for _, group := range query.Groups {
			if err := board.ReadBlockedMembers(r.Context(), p.board, group, out.Blocked); err != nil {
				return outcome{err: err}
			}
		}
		return outcome{status: http.StatusOK, body: out}
	})
}
