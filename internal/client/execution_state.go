package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/internal/api"
)

func (p *Project) ExecutionState(ctx context.Context, task string, proof Claim) (api.ExecutionState, error) {
	var out api.ExecutionState
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", url.PathEscape(task), "execution"), claim: &proof}, &out)
	return out, err
}
