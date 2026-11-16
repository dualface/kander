package remote

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

// showDocumentFixture serves one card document and runs RunShow against it.
func showDocumentFixture(t *testing.T) (run func(args ...string) (int, string, string), requested *[]string) {
	t.Helper()
	t.Setenv(EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(EnvExecutionID, "")
	_ = os.Unsetenv(EnvExecutionID)
	t.Setenv(EnvCoordinatorExecutionID, "")
	_ = os.Unsetenv(EnvCoordinatorExecutionID)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Chdir(t.TempDir())
	paths := []string{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()})
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(api.Project{ID: api.ProjectID("doc"), Name: "doc"})
		case strings.Contains(r.URL.Path, "/documents/"):
			paths = append(paths, r.URL.EscapedPath())
			name := r.URL.Path[strings.Index(r.URL.Path, "/documents/")+len("/documents/"):]
			if name != "plan.md" && name != "说明/a b.md" {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: "document " + name + " does not exist"})
				return
			}
			_ = json.NewEncoder(w).Encode(api.Document{Path: name, Text: "# Plan\n\nbody\n", SHA256: "x", ExpectedRevision: 7})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	cfg.Server.URL = hs.URL
	cfg.Project.Name = "doc"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := stdout, stderr
	t.Cleanup(func() { stdout, stderr = oldOut, oldErr })
	return func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		stdout, stderr = &out, &errOut
		code := RunShow(args)
		return code, out.String(), errOut.String()
	}, &paths
}

func TestShowDocumentPrintsOnlyTheDocument(t *testing.T) {
	run, requested := showDocumentFixture(t)
	code, out, errOut := run("--document", "plan.md", "20261116-a-task")
	if code != 0 || out != "# Plan\n\nbody\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = run("--json", "--document", "说明/a b.md", "20261116-a-task")
	var doc api.Document
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc.Path != "说明/a b.md" || doc.ExpectedRevision != 7 || doc.Text != "# Plan\n\nbody\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if len(*requested) != 2 || !strings.HasSuffix((*requested)[1], "/documents/%E8%AF%B4%E6%98%8E/a%20b.md") {
		t.Fatalf("requests=%v", *requested)
	}
}

func TestShowDocumentRejections(t *testing.T) {
	run, _ := showDocumentFixture(t)
	for name, args := range map[string][]string{
		"no value":    {"20261116-a-task", "--document"},
		"given twice": {"--document", "plan.md", "--document", "plan.md", "20261116-a-task"},
		"no task":     {"--document", "plan.md"},
	} {
		if code, out, errOut := run(args...); code != 2 || out != "" || errOut == "" {
			t.Fatalf("%s: code=%d out=%q err=%q", name, code, out, errOut)
		}
	}
	if code, out, errOut := run("--document", "missing.md", "20261116-a-task"); code == 0 || out != "" || !strings.Contains(errOut, "missing.md") {
		t.Fatalf("missing document: code=%d out=%q err=%q", code, out, errOut)
	}
}
