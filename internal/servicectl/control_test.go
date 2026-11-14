package servicectl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/fs"
)

func TestStopTimeoutAndRemoteRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := fs.WriteTextAtomic(dir, filepath.Join(dir, api.AdminTokenFile), `{"token":"`+strings.Repeat("a", 64)+`"}`, false); err != nil {
		t.Fatal(err)
	}
	var requested bool
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/server":
			_ = json.NewEncoder(w).Encode(api.ServerInfo{DataDir: dir})
		case "/api/v1/server/shutdown":
			requested = true
			w.WriteHeader(http.StatusAccepted)
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(api.Health{Ready: true})
		}
	}))
	defer hs.Close()
	ctl := Controller{Timeout: 100 * time.Millisecond}
	if _, err := ctl.Stop(t.Context(), hs.URL); err == nil || !requested {
		t.Fatalf("timeout=%v requested=%v", err, requested)
	}
	if _, err := ctl.Stop(t.Context(), "http://example.invalid"); err == nil {
		t.Fatal("remote stop allowed")
	}
	if _, err := ctl.Restart(t.Context(), "http://example.invalid"); err == nil {
		t.Fatal("remote restart allowed")
	}
}

func TestPathsAndListenDefaults(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if dir, err := DataDir(""); err != nil || dir != filepath.Join(os.Getenv("XDG_DATA_HOME"), "kander-server") {
		t.Fatalf("data=%s %v", dir, err)
	}
	for _, tc := range []struct {
		url, listen string
		local       bool
	}{
		{"http://localhost:1234", "localhost:1234", true}, {"http://[::1]:1234", "[::1]:1234", true}, {"http://127.0.0.1:1234", "127.0.0.1:1234", true},
		{"http://example.invalid:1234", api.DefaultListen, false}, {"http://127.0.0.1.evil:1234", api.DefaultListen, false},
	} {
		if Local(tc.url) != tc.local || ListenAddress(tc.url) != tc.listen {
			t.Fatalf("URL=%s", tc.url)
		}
	}
	for _, tc := range []struct{ listen, url string }{{":1234", "http://127.0.0.1:1234"}, {"0.0.0.0:1234", "http://127.0.0.1:1234"}, {"[::]:1234", "http://[::1]:1234"}} {
		if got, err := ProbeURL(tc.listen); err != nil || got != tc.url {
			t.Fatalf("probe=%s %v", got, err)
		}
	}
}
