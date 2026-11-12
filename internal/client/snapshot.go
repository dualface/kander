package client

import (
	"context"
	"github.com/dualface/kander/internal/api"
	"net/http"
)

func (p *Project) Snapshot(ctx context.Context, query api.SnapshotQuery) (api.BoardSnapshot, error) {
	var out api.BoardSnapshot
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("snapshot"), body: query}, &out)
	return out, err
}

func (p *Project) Check(ctx context.Context, query api.CheckQuery) (api.CheckResult, error) {
	var out api.CheckResult
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("check"), body: query}, &out)
	return out, err
}
