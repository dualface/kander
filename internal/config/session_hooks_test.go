package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredSessionHooksDocumented(t *testing.T) {
	root := repoRootForHooks(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "custom-agents.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)
	_, catalog, found := strings.Cut(body, "## Hook Catalog\n")
	if !found {
		t.Fatal("docs/custom-agents.md is missing the hook catalog heading")
	}
	catalog, _, _ = strings.Cut(catalog, "\n## ")
	for _, hook := range RegisteredSessionHooks() {
		if !strings.Contains(catalog, "| `"+hook.Name+"` |") {
			t.Fatalf("docs/custom-agents.md does not list hook %q", hook.Name)
		}
	}
}

func repoRootForHooks(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
