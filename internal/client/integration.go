package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/api"
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

// TaskEvidenceCleanup reads the cleanup scope of the Git evidence of a card;
// a card of a task group answers with its group.
func (p *Project) TaskEvidenceCleanup(ctx context.Context, task string) (api.EvidenceCleanup, error) {
	var value api.EvidenceCleanup
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", task, "evidence-cleanup")}, &value)
	return value, err
}

// GroupEvidenceCleanup reads the cleanup scope of the Git evidence of a task
// group.
func (p *Project) GroupEvidenceCleanup(ctx context.Context, group string) (api.EvidenceCleanup, error) {
	var value api.EvidenceCleanup
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("groups", url.PathEscape(group), "evidence-cleanup")}, &value)
	return value, err
}
