package client_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dualface/kander/client"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestNewUsesInjectedDefaultTransport(t *testing.T) {
	var paths []string
	client.SetDefaultTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "kander.invalid" {
			t.Errorf("host = %q", r.URL.Host)
		}
		paths = append(paths, r.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"server_version":"injected"}`))}, nil
	}))
	t.Cleanup(func() { client.SetDefaultTransport(nil) })
	for _, id := range []string{"client-a", "client-b"} {
		c, err := client.New("https://kander.invalid", id)
		if err != nil {
			t.Fatal(err)
		}
		version, err := c.Version(t.Context())
		if err != nil || version.ServerVersion != "injected" {
			t.Fatalf("version = %+v, %v", version, err)
		}
	}
	if want := "/api/v1/version,/api/v1/version"; strings.Join(paths, ",") != want {
		t.Fatalf("paths = %q, want %s", paths, want)
	}
}
