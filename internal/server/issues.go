package server

import (
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/issue"
)

func (s *Server) issueRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/issues"
	s.projectRoute(mux, "GET "+base+"/index", func(p *project, r *http.Request) outcome {
		index, err := issue.LoadIndex(p.board)
		if err != nil {
			return outcome{err: err}
		}
		for key, card := range index {
			card.Path = "kander-server:" + p.info.Name + "/" + card.TaskID
			index[key] = card
		}
		return outcome{status: http.StatusOK, body: index}
	})
	s.projectRoute(mux, "POST "+base+"/import", func(p *project, r *http.Request) outcome {
		var request api.IssueImport
		if err := decode(r, &request); err != nil {
			return outcome{err: err}
		}
		record, err := issue.UnmarshalImportSnapshot(request.Snapshot)
		if err != nil {
			return outcome{err: apiError(api.CodeInvalidRequest, err.Error())}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		run := func() outcome {
			imported, err := issue.PublishImport(p.board, record, issue.ImportOptions{Type: request.Type, Language: request.Language, Large: request.Large})
			if err != nil {
				return outcome{err: err}
			}
			imported.Path = "kander-server:" + p.info.Name + "/" + imported.TaskID
			p.changed(imported.TaskID, 0)
			return outcome{status: http.StatusOK, body: imported}
		}
		// The importer rechecks source-key uniqueness inside its exclusive board
		// transaction, including after a crash before the HTTP receipt was saved.
		return p.idempotent(r.Header.Get(api.HeaderIdempotencyKey), "issue-import", request, run, func() (outcome, bool) { return outcome{}, false })
	})
}
