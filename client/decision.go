package client

import (
	"context"

	"github.com/dualface/kander/api"
)

// RecordDecision writes one human or Leader decision.
func (p *Project) RecordDecision(ctx context.Context, key, id string, kind api.DecisionKind, req api.DecisionInput) (api.DecisionRecord, error) {
	return p.impl.RecordDecision(ctx, key, id, kind, req)
}

// Decisions lists a card's decisions in publication order.
func (p *Project) Decisions(ctx context.Context, id string) (api.DecisionList, error) {
	return p.impl.Decisions(ctx, id)
}

// SetDecisionMode changes a card's managed decision policy.
func (p *Project) SetDecisionMode(ctx context.Context, key, id string, req api.SetDecisionMode) (api.DecisionModeResult, error) {
	return p.impl.SetDecisionMode(ctx, key, id, req)
}
