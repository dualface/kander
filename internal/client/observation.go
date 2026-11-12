package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/internal/api"
)

func (p *Project) ObserveAgent(ctx context.Context, key, task string, observation api.AgentObservation, proof Claim) (api.ClaimView, error) {
	var out api.ClaimView
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("tasks", url.PathEscape(task), "observation"), key: key, body: observation, claim: &proof}, &out)
	return out, err
}
