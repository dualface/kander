package server

import (
	"context"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/i18n"
)

func (s *Server) TaskAccess(ctx context.Context, projectID, task string) (api.TaskAccess, error) {
	if !s.ready.Load() {
		return api.TaskAccess{}, apiError(api.CodeNotReady, "server is recovering")
	}
	p, err := s.project(projectID)
	if err != nil {
		return api.TaskAccess{}, err
	}
	if err := p.lockIndex(); err != nil {
		return api.TaskAccess{}, err
	}
	defer p.unlockBoard()
	id, err := board.NormalizeTaskID(task)
	if err != nil {
		return api.TaskAccess{}, asAPIError(err)
	}
	access, found, err := p.index.Access(ctx, id)
	if err != nil {
		return api.TaskAccess{}, p.indexReadError(ctx, err)
	}
	if !found {
		return api.TaskAccess{}, apiError(api.CodeNotFound, i18n.Text("en", "server.index_task_not_found"))
	}
	return access, nil
}
