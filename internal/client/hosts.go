package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dualface/kander/v2/api"
)

// Heartbeat reports this host to the server and long-polls for up to wait
// for a start order that targets it.
func (c *Client) Heartbeat(ctx context.Context, hb api.HostHeartbeat, wait time.Duration) (api.HostHeartbeatResult, error) {
	var out api.HostHeartbeatResult
	q := url.Values{"wait": {strconv.Itoa(int(wait / time.Second))}}
	err := c.do(ctx, call{method: http.MethodPost, path: api.HostHeartbeatRoute, query: q, body: hb}, &out)
	return out, err
}

// Hosts lists the hosts that sent heartbeats to the server.
func (c *Client) Hosts(ctx context.Context) (api.HostList, error) {
	var out api.HostList
	err := c.do(ctx, call{method: http.MethodGet, path: api.HostsRoute}, &out)
	return out, err
}

// CreateStartOrder asks the server to have a host start a todo card.
func (p *Project) CreateStartOrder(ctx context.Context, key string, req api.NewStartOrder) (api.StartOrder, error) {
	var o api.StartOrder
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("start-orders"), body: req, key: key}, &o)
	return o, err
}

// StartOrder reads one start order.
func (p *Project) StartOrder(ctx context.Context, id string) (api.StartOrder, error) {
	var o api.StartOrder
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path("start-orders", url.PathEscape(id))}, &o)
	return o, err
}

// AcceptStartOrder takes an order as its target host.
func (p *Project) AcceptStartOrder(ctx context.Context, key, id string) (api.StartOrder, error) {
	var o api.StartOrder
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("start-orders", url.PathEscape(id), "accept"), key: key}, &o)
	return o, err
}

// FailStartOrder reports, as the target host, a start that failed before its claim.
func (p *Project) FailStartOrder(ctx context.Context, key, id string, failure api.StartOrderFailure) (api.StartOrder, error) {
	var o api.StartOrder
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("start-orders", url.PathEscape(id), "fail"), body: failure, key: key}, &o)
	return o, err
}

// CancelStartOrder withdraws an order no claim took yet.
func (p *Project) CancelStartOrder(ctx context.Context, key, id string) (api.StartOrder, error) {
	var o api.StartOrder
	err := p.c.do(ctx, call{method: http.MethodPost, path: p.path("start-orders", url.PathEscape(id), "cancel"), key: key}, &o)
	return o, err
}
