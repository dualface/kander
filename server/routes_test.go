package server_test

import (
	"slices"
	"testing"

	"github.com/dualface/kander/v2/server"
)

func TestRoutesAreIndependentAndMarkOnlyProbesExempt(t *testing.T) {
	routes := server.Routes()
	var exempt []string
	for _, r := range routes {
		if !r.Authorized {
			exempt = append(exempt, r.Pattern)
		}
	}
	slices.Sort(exempt)
	want := []string{"GET /api/v1/health", "GET /api/v1/ready", "GET /api/v1/version"}
	if !slices.Equal(exempt, want) {
		t.Fatalf("authorization-exempt routes = %v, want %v", exempt, want)
	}
	t.Logf("public route count: %d", len(routes))
	routes[0] = server.Route{}
	if again := server.Routes(); len(again) != len(routes) || again[0] == routes[0] || !slices.Equal(again[1:], routes[1:]) {
		t.Fatalf("caller mutation affected later inventories: %v", again)
	}
}
