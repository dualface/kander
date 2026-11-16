package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
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
