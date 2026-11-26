package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
)

// UploadImport sends a single, non-replayable board archive. It never
// redirects the board to another endpoint or retries a partial request.
func (c *Client) UploadImport(ctx context.Context, project string, archive io.ReadCloser, progress func(api.ImportProgress)) (api.ImportResult, error) {
	var out api.ImportResult
	ctx, cancel := context.WithTimeout(ctx, api.ImportUploadTimeout+time.Minute)
	defer cancel()
	body := &importUploadBody{Reader: archive, close: sync.OnceValue(archive.Close)}
	stopClosing := context.AfterFunc(ctx, func() { _ = body.Close() })
	defer stopClosing()
	defer body.Close()
	if !api.ValidProjectName(project) {
		return out, &api.Error{Code: api.CodeInvalidRequest, Message: api.ProjectNameRule}
	}
	u := *c.base
	u.Path += strings.Replace(api.ImportUploadRoute, "{name}", project, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return out, err
	}
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderClientID, c.clientID)
	req.Header.Set("Content-Type", api.ImportUploadType)
	req.Header.Set("Accept", api.ImportStreamType)
	req.Header.Set("Expect", "100-continue")
	h := *c.http
	h.Timeout = 0 // The request context also interrupts the archive producer.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		return out, &TransportError{Err: err}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return out, ErrImportUnsupported
	case http.StatusOK:
		if !importStreamed(resp) {
			return out, &api.Error{Code: api.CodeInternal, Message: "unexpected board upload response"}
		}
		return readImportStream(resp.Body, progress)
	default:
		e := &api.Error{Status: resp.StatusCode}
		d := json.NewDecoder(io.LimitReader(resp.Body, importLineLimit))
		if err := d.Decode(e); err != nil || e.Code == "" {
			return out, &api.Error{Code: api.CodeInternal, Status: resp.StatusCode, Message: "board upload refused"}
		}
		return out, e
	}
}

type importUploadBody struct {
	io.Reader
	close func() error
}

func (b *importUploadBody) Close() error { return b.close() }
