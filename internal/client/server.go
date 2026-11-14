package client

import (
	"context"
	"github.com/dualface/kander/internal/api"
	"net/http"
)

func (c *Client) Health(ctx context.Context) (api.Health, error) {
	var out api.Health
	err := c.do(ctx, call{method: http.MethodGet, path: "/api/v1/health"}, &out)
	return out, err
}

func (c *Client) ServerInfo(ctx context.Context) (api.ServerInfo, error) {
	var out api.ServerInfo
	err := c.do(ctx, call{method: http.MethodGet, path: "/api/v1/server"}, &out)
	return out, err
}

// Shutdown never follows redirects: the local administration secret must not
// be forwarded to another endpoint. Error responses cannot echo the secret.
func (c *Client) Shutdown(ctx context.Context, token string) error {
	u := *c.base
	u.Path += "/api/v1/server/shutdown"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set(api.HeaderAdminToken, token)
	h := *c.http
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		return &TransportError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return &api.Error{Code: api.CodeInvalidRequest, Message: "server shutdown refused", Status: resp.StatusCode}
	}
	return nil
}
