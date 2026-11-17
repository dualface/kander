package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dualface/kander/internal/api"
)

// A client refuses a server that only speaks protocol 1, which predates
// recoverable lapsed leases.
func TestCheckCompatibleRefusesAProtocolOneServer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":1,"min_protocol":1,"server_version":"old"}`))
	}))
	defer old.Close()
	c, err := New(old.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	if _, err := c.CheckCompatible(t.Context()); !api.IsCode(err, api.CodeProtocolIncompatible) {
		t.Fatalf("a protocol 1 server was accepted: %v", err)
	}
}
