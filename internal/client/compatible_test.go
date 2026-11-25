package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dualface/kander/api"
)

// A client refuses a protocol 2 server instead of decoding its old assignee.
func TestCheckCompatibleRefusesAProtocolTwoServer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":2,"min_protocol":2,"server_version":"old"}`))
	}))
	defer old.Close()
	c, err := New(old.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	if _, err := c.CheckCompatible(t.Context()); !api.IsCode(err, api.CodeProtocolIncompatible) {
		t.Fatalf("a protocol 2 server was accepted: %v", err)
	}
}
