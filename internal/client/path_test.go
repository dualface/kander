package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A document name is escaped once on the wire and decodes to the original name.
func TestDocumentPathIsEscapedOnce(t *testing.T) {
	for _, name := range []string{"plan.md", "notes/a b.md", "说明/计划.md", "a#b?c%d.md", "dir/语 言.md"} {
		var escaped, decoded string
		hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			escaped, decoded = r.URL.EscapedPath(), r.URL.Path
			_, _ = w.Write([]byte(`{"path":` + `"` + strings.ReplaceAll(name, `"`, `\"`) + `"}`))
		}))
		c, err := New(hs.URL, "path-test")
		if err != nil {
			t.Fatal(err)
		}
		c.Retries = 0
		doc, err := c.Project("p 1").Document(t.Context(), "20261116-a-task", name)
		hs.Close()
		if err != nil || doc.Path != name {
			t.Fatalf("%q: doc=%+v err=%v", name, doc, err)
		}
		want := "/api/v1/projects/p%201/tasks/20261116-a-task/documents/" + documentPath(name)
		if escaped != want {
			t.Fatalf("%q: wire path %q, want %q", name, escaped, want)
		}
		if !strings.HasSuffix(decoded, "/documents/"+name) || !strings.HasPrefix(decoded, "/api/v1/projects/p 1/") {
			t.Fatalf("%q: server decodes %q", name, decoded)
		}
	}
}

// A path prefix in the server URL stays in front of the request path.
func TestBasePathPrefixSurvives(t *testing.T) {
	var escaped string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer hs.Close()
	c, err := New(hs.URL+"/proxy%20x", "path-test")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	if _, err := c.Project("p").Document(t.Context(), "20261116-a-task", "a b.md"); err != nil && !strings.Contains(err.Error(), "decode") {
		t.Fatal(err)
	}
	if want := "/proxy%20x/api/v1/projects/p/tasks/20261116-a-task/documents/a%20b.md"; escaped != want {
		t.Fatalf("wire path %q, want %q", escaped, want)
	}
}
