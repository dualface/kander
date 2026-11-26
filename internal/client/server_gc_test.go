package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

// A gc call outlasts the client's default timeout: the server may run a gc
// far longer than an ordinary request.
func TestServerGCOutlastsTheDefaultTimeout(t *testing.T) {
	var got api.ServerGCRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/gc" || r.Header.Get(api.HeaderAdminToken) != "token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		time.Sleep(300 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(api.ServerGCReport{DryRun: got.DryRun, Projects: []api.ServerGCProject{{Name: "demo"}}})
	}))
	defer srv.Close()
	c, err := New(srv.URL, "gc-test")
	if err != nil {
		t.Fatal(err)
	}
	// Stand-ins for the 90-second default and the gc timeout.
	c.http.Timeout = 50 * time.Millisecond
	defer func(previous time.Duration) { gcTimeout = previous }(gcTimeout)
	gcTimeout = 5 * time.Second
	report, err := c.ServerGC(t.Context(), "token", api.ServerGCRequest{Project: "demo", DryRun: true, OlderThan: "168h0m0s", LogKeep: 1 << 26})
	if err != nil || !report.DryRun || len(report.Projects) != 1 || got.Project != "demo" || got.LogKeep != 1<<26 {
		t.Fatalf("report = %+v, request = %+v, err = %v", report, got, err)
	}
	gcTimeout = 50 * time.Millisecond
	if _, err := c.ServerGC(t.Context(), "token", api.ServerGCRequest{OlderThan: "1h"}); !IsUnreachable(err) {
		t.Fatalf("a call past the gc timeout must fail as a transport error, got %v", err)
	}
}

func TestServerGCReturnsTheRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeGCRunning, Message: "busy"})
	}))
	defer srv.Close()
	c, err := New(srv.URL, "gc-test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ServerGC(t.Context(), "token", api.ServerGCRequest{OlderThan: "1h"})
	if !api.IsCode(err, api.CodeGCRunning) {
		t.Fatalf("want gc_running, got %v", err)
	}
}
