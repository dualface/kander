package client

import (
	"context"
	"net/http"

	"github.com/dualface/kander/internal/api"
)

func (p *Project) IssueResult(ctx context.Context, input api.IssueResultQuery) (api.IssueResultState, error) {
	var value api.IssueResultState
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("issues", "results", "view"), body: input}, &value)
	return value, err
}
func (p *Project) SaveIssueResult(ctx context.Context, key string, input api.IssueResultWrite) (api.IssueResultState, error) {
	var value api.IssueResultState
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("issues", "results", "save"), body: input, key: key}, &value)
	return value, err
}
