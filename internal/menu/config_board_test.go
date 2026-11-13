//go:build unix

package menu

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/internal/api"
)

const configBoardPath = "/srv/kander/projects/golf-625fe74cad4600b5/board"

// projectServer answers GET /api/v1/projects?name=golf and records every request.
type projectServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newProjectServer(t *testing.T) *projectServer {
	t.Helper()
	s := &projectServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects" && r.URL.Query().Get("name") == "golf" {
			_ = json.NewEncoder(w).Encode(api.Project{ID: api.ProjectID("golf"), Name: "golf", BoardPath: configBoardPath})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: "project is not registered"})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *projectServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// bindProject writes a project overlay holding the binding and returns the checkout.
func bindProject(t *testing.T, h *harness, url, name string) string {
	t.Helper()
	dir := filepath.Join(h.root, "checkout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	binding := map[string]any{}
	if url != "" {
		binding["server"] = map[string]any{"url": url}
	}
	if name != "" {
		binding["project"] = map[string]any{"name": name}
	}
	data, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kander-config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// homeFiles lists every file below the harness home except the scope config.
func homeFiles(t *testing.T, h *harness) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(h.home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == h.configPath {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestConfigPrintsTheServerBoardPath(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en"}))
	srv := newProjectServer(t)
	dir := bindProject(t, h, srv.URL, "golf")
	before := homeFiles(t, h)
	code, out, errOut := h.runIn(dir, "config")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if !strings.Contains(out, ": "+configBoardPath) {
		t.Fatalf("output lacks the board path:\n%s", out)
	}
	for _, request := range srv.seen() {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("config sent %s", request)
		}
	}
	if after := homeFiles(t, h); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("config wrote files: before %v, after %v", before, after)
	}
}

func TestConfigShowsAnUnavailableBoardPath(t *testing.T) {
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()
	for name, binding := range map[string][2]string{
		"unreachable":  {unreachable.URL, "golf"},
		"unbound":      {"", ""},
		"unregistered": {"", "tennis"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig(defaultPayload(map[string]any{"language": "en"}))
			url := binding[0]
			if name == "unregistered" {
				url = newProjectServer(t).URL
			}
			dir := bindProject(t, h, url, binding[1])
			code, out, errOut := h.runIn(dir, "config")
			if code != 0 {
				t.Fatalf("%d %s", code, errOut)
			}
			if !strings.Contains(out, "Server board path (on the server host): unavailable") {
				t.Fatalf("output lacks the unavailable board path:\n%s", out)
			}
		})
	}
}

func TestConfigJSONDoesNotContactTheServer(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en"}))
	srv := newProjectServer(t)
	dir := bindProject(t, h, srv.URL, "golf")
	code, out, errOut := h.runIn(dir, "config", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if strings.Contains(out, "board_path") || strings.Contains(out, configBoardPath) {
		t.Fatalf("JSON output changed:\n%s", out)
	}
	if requests := srv.seen(); len(requests) != 0 {
		t.Fatalf("config --json contacted the server: %v", requests)
	}
}

// The binding is in force before the welcome completes, so config shows it.
func TestConfigShowsTheBindingBeforeTheWelcomeCompletes(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en", "welcome_complete": false}))
	dir := bindProject(t, h, "http://127.0.0.1:1", "golf")
	code, out, errOut := h.runIn(dir, "--lang", "en", "config")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	for _, want := range []string{"Server URL: http://127.0.0.1:1", "Project name: golf"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
}
