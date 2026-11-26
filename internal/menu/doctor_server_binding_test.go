//go:build unix

package menu

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// The doctor repair path must keep a valid server binding in the scope file and
// leave the project overlay file untouched.
func TestRepairDoctorConfigKeepsServerBinding(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	scopePath := filepath.Join(root, "scope", "config.json")
	t.Setenv(config.EnvConfig, scopePath)

	// The tool repair has work to do (codex is not usable), so the scope file is rewritten.
	payload := defaultPayload(map[string]any{
		"kanban_agent": "codex",
		"language":     "en",
		"server":       map[string]any{"url": "http://127.0.0.1:7979"},
		"project":      map[string]any{"name": "demo"},
	})
	h := &harness{t: t, configPath: scopePath}
	h.writeConfig(payload)

	project := filepath.Join(root, "project")
	initGitDir(t, project)
	overlayPath, overlayBytes := writeOverlayFile(t, project, map[string]any{"launcher": "foreground"})
	t.Chdir(project)

	repaired, ok := repairDoctorConfig(usableAgentsFixture(), TerminalTools{}, false)
	if !ok || repaired == nil {
		t.Fatal("repair failed")
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.KanbanAgent != "claude" {
		t.Fatalf("the repair did not rewrite the scope file: %+v", saved)
	}
	if saved.Server.URL != "http://127.0.0.1:7979" || saved.Project.Name != "demo" {
		t.Fatalf("scope file lost the server binding: %+v %+v", saved.Server, saved.Project)
	}
	if after, err := os.ReadFile(overlayPath); err != nil || !bytes.Equal(after, overlayBytes) {
		t.Fatalf("overlay file changed: %s %v", after, err)
	}
}
