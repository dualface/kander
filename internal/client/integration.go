package client

import (
	"context"
	"net/http"

	"github.com/dualface/kander/internal/api"
)

func (p *Project) Integration(ctx context.Context, id string) (api.Integration, error) {
	var value api.Integration
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("integrations", id)}, &value)
	return value, err
}
func (p *Project) ClaimIntegration(ctx context.Context, key string, input api.IntegrationRequest, authority Claim) (api.IntegrationGrant, error) {
	var value api.IntegrationGrant
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("integrations", api.IntegrationID(input.TargetRef), "claim"), body: input, key: key, claim: &authority}, &value)
	return value, err
}
func (p *Project) IntegrationAction(ctx context.Context, key, id string, input api.IntegrationCommand, proof Claim) (api.Integration, error) {
	var value api.Integration
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("integrations", id), body: input, key: key, claim: &proof}, &value)
	return value, err
}
func (p *Project) RenewIntegration(ctx context.Context, id string, proof Claim) (api.Renewal, error) {
	var value api.Renewal
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("integrations", id, "renew"), claim: &proof}, &value)
	return value, err
}

func (p *Project) IntegrationObservation(ctx context.Context, id string) (api.IntegrationObservation, error) {
	var value api.IntegrationObservation
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("integrations", id, "observation")}, &value)
	return value, err
}

func (p *Project) IntegrationExecution(ctx context.Context, id, execution string) (api.Integration, error) {
	var value api.Integration
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("integrations", id, "executions", execution)}, &value)
	return value, err
}

func (p *Project) TaskIntegration(ctx context.Context, task string) (api.Integration, error) {
	var value api.Integration
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", task, "integration")}, &value)
	return value, err
}
