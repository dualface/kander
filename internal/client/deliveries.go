package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/dualface/kander/internal/api"
)

func (p *Project) PublishDelivery(ctx context.Context, key string, input api.DeliveryEvidence, proof Claim) (api.DeliveryEvidence, error) {
	var out api.DeliveryEvidence
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("tasks", url.PathEscape(input.TaskID), "deliveries"), key: key, body: input, claim: &proof}, &out)
	return out, err
}

func (p *Project) Delivery(ctx context.Context, task, commit string) (api.DeliveryEvidence, error) {
	var out api.DeliveryEvidence
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("tasks", url.PathEscape(task), "deliveries", url.PathEscape(commit))}, &out)
	return out, err
}
