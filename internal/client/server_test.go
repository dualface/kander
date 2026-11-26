package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
)

func TestImportKeepsTheTokenOnItsEndpoint(t *testing.T) {
	token := strings.Repeat("b", 64)
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get(api.HeaderAdminToken) != ""
	}))
	defer elsewhere.Close()
	redirect := httptest.NewServer(http.RedirectHandler(elsewhere.URL+"/api/v1/server/import", http.StatusTemporaryRedirect))
	defer redirect.Close()
	c, err := New(redirect.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Import(t.Context(), token, api.ImportRequest{Project: "demo", From: "/kanban"}); err == nil || leaked {
		t.Fatalf("redirect followed: err=%v leaked=%v", err, leaked)
	}
}

func TestImportReportsAServerWithoutTheRoute(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	c, err := New(old.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Import(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}); !errors.Is(err, ErrImportUnsupported) {
		t.Fatalf("missing route: %v", err)
	}
}

// streamServer answers every import request with the given lines.
func streamServer(t *testing.T, contentType string, status int, lines ...string) *Client {
	t.Helper()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		for _, line := range lines {
			w.Write([]byte(line + "\n"))
		}
	}))
	t.Cleanup(h.Close)
	c, err := New(h.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestImportStreamDeliversProgressThenTheResult(t *testing.T) {
	c := streamServer(t, api.ImportStreamType, http.StatusOK,
		`{"progress":{"phase":"scan"}}`,
		`{"progress":{"phase":"copy","done":1,"total":2,"bytes":10,"total_bytes":20}}`,
		`{"result":{"project":{"id":"p-1"},"files":2,"tasks":1,"snapshot":"s","defaulted_size":["a-task"],"warnings":[{"task_ids":["b-task"],"message":"m"}]}}`)
	var got []api.ImportProgress
	result, err := c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, func(p api.ImportProgress) { got = append(got, p) })
	if err != nil || result.Tasks != 1 || len(result.DefaultedSize) != 1 || result.Warnings[0].TaskIDs[0] != "b-task" {
		t.Fatalf("result: %+v %v", result, err)
	}
	if len(got) != 2 || got[0].Phase != "scan" || got[1].Done != 1 || got[1].TotalBytes != 20 {
		t.Fatalf("progress: %+v", got)
	}
}

func TestImportStreamErrorKeepsItsStatus(t *testing.T) {
	c := streamServer(t, api.ImportStreamType, http.StatusOK,
		`{"progress":{"phase":"lock"}}`,
		`{"status":409,"error":{"code":"project_not_empty","message":"full"}}`)
	_, err := c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, func(api.ImportProgress) {})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "project_not_empty" || apiErr.Status != http.StatusConflict {
		t.Fatalf("error: %#v", err)
	}
}

func TestImportStreamWithoutAnEndingLineIsATransportFailure(t *testing.T) {
	c := streamServer(t, api.ImportStreamType, http.StatusOK, `{"progress":{"phase":"copy","done":1,"total":9}}`)
	_, err := c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, func(api.ImportProgress) {})
	var transport *TransportError
	if !errors.As(err, &transport) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a cut stream must not read as success or as a server answer: %#v", err)
	}
}

func TestImportStreamRefusesAnOversizedLine(t *testing.T) {
	c := streamServer(t, api.ImportStreamType, http.StatusOK, `{"progress":{"phase":"`+strings.Repeat("x", importLineLimit)+`"}}`)
	if _, err := c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, func(api.ImportProgress) {}); err == nil {
		t.Fatal("an oversized line must fail the import call")
	}
}

func TestImportAgainstAServerWithoutTheStream(t *testing.T) {
	var accept string
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"project":{"id":"p-1"},"files":3,"tasks":2,"snapshot":"s"}` + "\n"))
	}))
	defer h.Close()
	c, _ := New(h.URL, "host-a")
	called := false
	result, err := c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, func(api.ImportProgress) { called = true })
	if err != nil || result.Tasks != 2 || called || !strings.Contains(accept, api.ImportStreamType) {
		t.Fatalf("an old server answers one JSON: %+v %v called=%v accept=%q", result, err, called, accept)
	}
	if _, err := c.Import(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}); err != nil || accept != "" {
		t.Fatalf("Import without a callback never asks for the stream: %v accept=%q", err, accept)
	}
}
