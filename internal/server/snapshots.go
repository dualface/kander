package server

import (
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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
		for _, group := range query.Groups {
			if !board.ValidTaskGroup(group) {
				return outcome{err: apiError(api.CodeInvalidRequest, "invalid snapshot group identity")}
			}
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
		out := api.BoardSnapshot{Coordinators: map[string]api.CoordinatorSummary{}, ObservedAt: p.server.now(), View: view, Claims: map[string]*api.ClaimView{}, Blocked: map[string]string{}, Cursor: p.events.cursor()}
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
			value, err := p.groupView(r.Context(), group)
			if err != nil {
				return outcome{err: err}
			}
			summary := api.CoordinatorSummary{GroupID: group, Claim: value.Claim}
			if checkpoint := value.Checkpoint; checkpoint != nil {
				summary.Revision, summary.Epoch, summary.ContentHash = checkpoint.Revision, checkpoint.Authority.Epoch, checkpoint.ContentHash
				for _, id := range checkpoint.BlockedMembers() {
					out.Blocked[id] = checkpoint.Members[id].Blocked.Reason
				}
			}
			out.Coordinators[group] = summary
		}
		return outcome{status: http.StatusOK, body: out}
	})
}
