package issue

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/version"
)

// policyImportServer answers the routes of an import through the service and records the
// language of the import request. Its project policy says ja.
func policyImportServer(t *testing.T) (string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var languages []string
	values := `{"git":{"branch_mode":"main-develop"},"review_stages":{"PMQA":"auto","Security":"auto"},"rules":{},"agent_language":"ja","issue":{"result_on_done":"auto"}}`
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()})
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(api.Project{ID: api.ProjectID("golf"), Name: "golf", Policy: &api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: json.RawMessage(values)}})
		case strings.HasSuffix(r.URL.Path, "/issues/index"):
			_, _ = w.Write([]byte("{}"))
		case strings.HasSuffix(r.URL.Path, "/issues/import"):
			var request api.IssueImport
			_ = json.NewDecoder(r.Body).Decode(&request)
			mu.Lock()
			languages = append(languages, request.Language)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ImportResult{TaskID: "20261127-imported-task", State: "backlog"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)
	return hs.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), languages...)
	}
}

// Without --language the card takes the agent language of the project policy, also on a host
// that has no policy cache yet; --language still wins.
func TestIssueImportUsesTheLanguageOfTheProjectPolicy(t *testing.T) {
	useChinese(t)
	url, languages := policyImportServer(t)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.AgentLanguage = "zh-CN"
	cfg.Server.URL, cfg.Project.Name = url, "golf"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repository := importTestRepository()
	provider := &stubResolver{repository: repository, snapshot: importTestSnapshot(repository, 7)}
	factory := func() IssueProvider { return provider }
	var out, errOut strings.Builder
	if code := runImport(factory, issueImportOptions{number: 7, repository: repository.Owner + "/" + repository.Name, hasRepo: true}, &out, &errOut); code != 0 {
		t.Fatalf("import: code=%d err=%q", code, errOut.String())
	}
	if code := runImport(factory, issueImportOptions{number: 7, repository: repository.Owner + "/" + repository.Name, hasRepo: true, language: "en"}, &out, &errOut); code != 0 {
		t.Fatalf("import --language: code=%d err=%q", code, errOut.String())
	}
	if got := languages(); len(got) != 2 || got[0] != "ja" || got[1] != "en" {
		t.Fatalf("import languages %v, want [ja en]", got)
	}
}
