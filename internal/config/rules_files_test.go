package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/rules"
)

func TestReadRulesFileWarnsOnlyOnFallback(t *testing.T) {
	paths := InstallPaths{Mode: ModeGlobal, RulesDir: t.TempDir()}
	var warn bytes.Buffer
	data, err := ReadRulesFile(paths, "snippets/README.md", &warn)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := rules.File("snippets/README.md"); string(data) != string(want) {
		t.Fatal("fallback must return the embedded copy")
	}
	if !strings.Contains(warn.String(), "kander doctor") || !strings.Contains(warn.String(), "snippets/README.md") {
		t.Fatalf("fallback hint missing: %q", warn.String())
	}
	if err := os.MkdirAll(filepath.Join(paths.RulesDir, "snippets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "snippets", "README.md"), []byte("installed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	warn.Reset()
	data, err = ReadRulesFile(paths, "snippets/README.md", &warn)
	if err != nil || string(data) != "installed\n" || warn.Len() != 0 {
		t.Fatalf("installed read: data=%q warn=%q err=%v", data, warn.String(), err)
	}
}

func TestRenderRulesTemplateFailsWhole(t *testing.T) {
	paths := InstallPaths{Mode: ModeGlobal, RulesDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(paths.RulesDir, "templates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "templates", "probe.md"), []byte("{{.A}}-{{.B}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	out, err := RenderRulesTemplate(paths, "templates/probe.md", map[string]string{"A": "1", "B": "2"}, &warn)
	if err != nil || out != "1-2" || warn.Len() != 0 {
		t.Fatalf("out=%q warn=%q err=%v", out, warn.String(), err)
	}
	if _, err := RenderRulesTemplate(paths, "templates/probe.md", map[string]string{"A": "1"}, &warn); err == nil {
		t.Fatal("missing key must fail")
	}
	out, err = RenderRulesTemplate(paths, "templates/README.md", map[string]string{}, &warn)
	if err != nil || out == "" || !strings.Contains(warn.String(), "kander doctor") {
		t.Fatalf("embedded template fallback: err=%v warn=%q", err, warn.String())
	}
}
