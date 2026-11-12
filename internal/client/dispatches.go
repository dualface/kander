package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *Project) Dispatch(ctx context.Context, task, id string) (board.Dispatch, error) {
	var out board.Dispatch
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", url.PathEscape(task), "dispatches", url.PathEscape(id))}, &out)
	return out, err
}
func (p *Project) DispatchAction(ctx context.Context, key, task, action string, input api.DispatchCommand, proof Claim) (board.Dispatch, error) {
	var out board.Dispatch
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("tasks", url.PathEscape(task), "dispatches", "actions", url.PathEscape(action)), key: key, body: input, claim: &proof}, &out)
	return out, err
}
func (p *Project) DispatchRange(ctx context.Context, task string) (board.ReviewGitEdge, error) {
	var out board.ReviewGitEdge
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", url.PathEscape(task), "dispatches", "review-range")}, &out)
	return out, err
}
func (p *Project) ValidateDispatch(ctx context.Context, task, id string) error {
	var out struct {
		Valid bool `json:"valid"`
	}
	return p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", url.PathEscape(task), "dispatches", url.PathEscape(id), "validate")}, &out)
}
