package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

func (p *Project) ReviewRuntime(ctx context.Context, key, run, action string, payload any, proof *Claim) (json.RawMessage, error) {
	path := p.path("reviews", "runtime", url.PathEscape(run), url.PathEscape(action))
	if action == "prepare" {
		path = p.path("reviews", "runtime", "prepare")
	}
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodPost, path: path, key: key, body: payload, claim: proof}, &out)
	return out, err
}
func (p *Project) ReviewEvidence(ctx context.Context, run, kind, name string) (json.RawMessage, error) {
	path := p.path("reviews", "evidence", url.PathEscape(run), url.PathEscape(kind))
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	var out json.RawMessage
	err := p.c.do(ctx, call{method: http.MethodGet, path: path}, &out)
	return out, err
}
func (p *Project) ReviewRemaining(ctx context.Context, ids []string) ([]string, error) {
	var out []string
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("reviews", "members"), body: ids}, &out)
	return out, err
}
