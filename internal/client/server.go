package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/api"
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

// importTimeout outlasts the server's 5-minute bound on the snapshot lock
// wait and the copy. Validation and publication are not bounded, so a timeout
// here does not tell whether the import was published.
const importTimeout = 6 * time.Minute

// ErrImportUnsupported reports a running server that predates online import.
var ErrImportUnsupported = errors.New("the running server does not support online import")

// importLineLimit bounds one line of a streamed import.
const importLineLimit = 1 << 20

// Import asks a local server to import a v1 board and waits for the single
// JSON answer. Like Shutdown it never follows redirects, so the administration
// token reaches only this endpoint.
func (c *Client) Import(ctx context.Context, token string, in api.ImportRequest) (api.ImportResult, error) {
	return c.ImportWithProgress(ctx, token, in, nil)
}

// ImportWithProgress is Import that asks the server to stream its progress to
// progress. A server that predates the stream answers with the single JSON,
// and progress is then never called.
func (c *Client) ImportWithProgress(ctx context.Context, token string, in api.ImportRequest, progress func(api.ImportProgress)) (api.ImportResult, error) {
	var out api.ImportResult
	payload, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	u := *c.base
	u.Path += "/api/v1/server/import"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderClientID, c.clientID)
	req.Header.Set(api.HeaderAdminToken, token)
	req.Header.Set("Content-Type", "application/json")
	if progress != nil {
		req.Header.Set("Accept", api.ImportStreamType)
	}
	h := *c.http
	h.Timeout = importTimeout
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		return out, &TransportError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && importStreamed(resp) {
		return readImportStream(resp.Body, progress)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, &TransportError{Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		e := &api.Error{Status: resp.StatusCode}
		if json.Unmarshal(data, e) == nil && e.Code != "" {
			return out, e
		}
		// An older server has no such route; its mux answers in plain text.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			return out, ErrImportUnsupported
		}
		e.Code, e.Message = api.CodeInternal, strings.TrimSpace(string(data))
		return out, e
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("server answer is malformed: %w", err)
	}
	return out, nil
}

func importStreamed(resp *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && mediaType == api.ImportStreamType
}

// readImportStream reads the lines of a streamed import up to its last line.
// A stream that ends without a result or an error is a transport failure: the
// server went away, and the import may or may not have been published.
func readImportStream(body io.Reader, progress func(api.ImportProgress)) (api.ImportResult, error) {
	var out api.ImportResult
	lines := bufio.NewScanner(body)
	lines.Buffer(nil, importLineLimit)
	for lines.Scan() {
		var line api.ImportStreamLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			return out, fmt.Errorf("server answer is malformed: %w", err)
		}
		switch {
		case line.Progress != nil:
			if progress != nil {
				progress(*line.Progress)
			}
		case line.Result != nil:
			return *line.Result, nil
		case line.Error != nil:
			line.Error.Status = line.Status
			return out, line.Error
		}
	}
	err := lines.Err()
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return out, &TransportError{Err: err}
}
