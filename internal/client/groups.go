package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *Project) Group(ctx context.Context, id string) (api.GroupView, error) {
	var out api.GroupView
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("groups", url.PathEscape(id))}, &out)
	return out, err
}
func (p *Project) ClaimGroup(ctx context.Context, key, id string, input api.GroupClaim, proof *Claim) (api.GroupGrant, error) {
	var out api.GroupGrant
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), "claim"), key: key, body: input, claim: proof}, &out)
	return out, err
}
func (p *Project) RenewGroup(ctx context.Context, id string, proof Claim) (api.Renewal, error) {
	var out api.Renewal
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), "renew"), claim: &proof}, &out)
	return out, err
}
func (p *Project) CoordinatorFacts(ctx context.Context, id string, input board.CoordinatorReconcile) ([]board.CoordinatorGitFacts, error) {
	var out []board.CoordinatorGitFacts
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), "facts"), body: input}, &out)
	return out, err
}
func (p *Project) CoordinatorAction(ctx context.Context, key, id, action string, input any, proof Claim) (board.CoordinatorCheckpoint, error) {
	var out board.CoordinatorCheckpoint
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), url.PathEscape(action)), key: key, body: input, claim: &proof}, &out)
	return out, err
}

func (p *Project) ObserveGroup(ctx context.Context, key, id string, input api.AgentObservation, proof Claim) (api.Renewal, error) {
	var out api.Renewal
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), "observation"), key: key, body: input, claim: &proof}, &out)
	return out, err
}
func (p *Project) ReleaseGroup(ctx context.Context, key, id string, input api.GroupRelease, proof Claim) (api.Renewal, error) {
	var out api.Renewal
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("groups", url.PathEscape(id), "release"), key: key, body: input, claim: &proof}, &out)
	return out, err
}
