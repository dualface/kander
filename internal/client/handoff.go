package client

import (
	"context"
	"net/http"

	"github.com/dualface/kander/internal/api"
)

// Handoff lets the current holder restart its stopped executor on the same
// client. A live foreign lease still refuses takeover, regardless of presence.
func (p *Project) Handoff(ctx context.Context, key, task string, input api.ClaimRequest, proof Claim) (api.ClaimGrant, error) {
	var value api.ClaimGrant
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(task, "takeover")...), body: input, key: key, claim: &proof}, &value)
	return value, err
}
