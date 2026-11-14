package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
)

func TestAdministrationAuthenticationAndNoSecretDisclosure(t *testing.T) {
	var log bytes.Buffer
	s, err := Open(Options{DataDir: t.TempDir(), Version: "test-version", Logger: slog.New(slog.NewJSONHandler(&log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	secret, err := os.ReadFile(filepath.Join(s.data, api.AdminTokenFile))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := os.ReadFile(filepath.Join(s.data, "secret.json"))
	if err != nil || bytes.Equal(secret, claim) {
		t.Fatal("administration token must be independent")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(s.data, api.AdminTokenFile))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("mode=%v %v", info, err)
		}
	}
	handler := s.Handler()
	for _, tc := range []struct{ peer, token string }{{"192.0.2.1:4567", s.adminToken}, {"127.0.0.1:4567", ""}, {"127.0.0.1:4567", strings.Repeat("a", 64)}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/server/shutdown", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set(api.HeaderAdminToken, tc.token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != http.StatusForbidden || strings.Contains(out.Body.String(), s.adminToken) {
			t.Fatalf("refusal=%d %s", out.Code, out.Body.String())
		}
		select {
		case <-s.shutdown:
			t.Fatal("refused request shut down server")
		default:
		}
		h := httptest.NewRecorder()
		handler.ServeHTTP(h, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
		if h.Code != 200 {
			t.Fatal("refusal affected server")
		}
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/server", nil))
	var info api.ServerInfo
	if err := json.Unmarshal(out.Body.Bytes(), &info); err != nil || info.DataDir != s.data || info.Version != "test-version" || info.PID != os.Getpid() {
		t.Fatalf("info=%s %v", out.Body.String(), err)
	}
	if strings.Contains(out.Body.String(), s.adminToken) || strings.Contains(log.String(), s.adminToken) {
		t.Fatal("token disclosed")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/server/shutdown", nil)
	req.RemoteAddr = "[::1]:4567"
	req.Header.Set(api.HeaderAdminToken, s.adminToken)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != 202 {
		t.Fatalf("shutdown=%d", out.Code)
	}
	select {
	case <-s.shutdown:
	default:
		t.Fatal("authenticated shutdown not requested")
	}
}
