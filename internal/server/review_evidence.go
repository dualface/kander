package server

import (
	"github.com/dualface/kander/v2/internal/board"
	"net/http"
)

func (s *Server) reviewEvidenceRoutes(mux *routeTable) {
	s.projectRoute(mux, "POST /api/v1/projects/{project}/reviews/members", func(p *project, r *http.Request) outcome {
		var ids []string
		if err := decode(r, &ids); err != nil {
			return outcome{err: err}
		}
		if err := p.lockBoard(); err != nil {
			return outcome{err: err}
		}
		defer p.unlockBoard()
		remaining, err := board.ReviewRemainingMembers(p.board, ids)
		return result(http.StatusOK, remaining, err)
	})
	const base = "/api/v1/projects/{project}/reviews/evidence/{run}"
	s.projectRoute(mux, "GET "+base+"/{kind}/{name}", func(p *project, r *http.Request) outcome {
		id, name := r.PathValue("run"), r.PathValue("name")
		var data []byte
		var err error
		switch r.PathValue("kind") {
		case "original":
			data, err = board.ReadReviewOriginal(p.board, id, name)
		case "input":
			data, err = board.ReadReviewInput(p.board, id, name)
		default:
			return outcome{err: apiError("invalid_request", "unsupported review evidence kind")}
		}
		return result(http.StatusOK, data, err)
	})
	s.projectRoute(mux, "GET "+base+"/incremental", func(p *project, r *http.Request) outcome {
		data, err := board.ReviewIncrementalContext(p.board, r.PathValue("run"))
		return result(http.StatusOK, data, err)
	})
	s.projectRoute(mux, "GET "+base+"/status", func(p *project, r *http.Request) outcome {
		id := r.PathValue("run")
		if err := board.ReviewPublicationComplete(p.board, id); err != nil {
			return outcome{err: err}
		}
		pending, err := board.ReviewNeedsInterpretation(p.board, id)
		return result(http.StatusOK, map[string]bool{"needs_interpretation": pending}, err)
	})
}
