package client

import (
	"context"
	"net/http"

	"github.com/dualface/kander/api"
)

// RecordDecision writes to the route reserved for the selected decision kind.
func (p *Project) RecordDecision(ctx context.Context, key, id string, kind api.DecisionKind, req api.DecisionInput) (api.DecisionRecord, error) {
	var record api.DecisionRecord
	if !kind.Valid() {
		return record, &api.Error{Code: api.CodeInvalidRequest}
	}
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "decisions", string(kind))...), body: req, key: key}, &record)
	return record, err
}

// Decisions reads a card's decision records in publication order.
func (p *Project) Decisions(ctx context.Context, id string) (api.DecisionList, error) {
	var list api.DecisionList
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path(taskPath(id, "decisions")...)}, &list)
	return list, err
}

// SetDecisionMode changes the card's managed decision policy.
func (p *Project) SetDecisionMode(ctx context.Context, key, id string, req api.SetDecisionMode) (api.DecisionModeResult, error) {
	var changed api.DecisionModeResult
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path(taskPath(id, "decision-mode")...), body: req, key: key}, &changed)
	return changed, err
}
