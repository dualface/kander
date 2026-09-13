package terminalcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/terminal"
)

// Pins the selection order documented in docs/terminal-definitions.md:
// definitions in inventory order, and within one definition a launcher name
// before the definition name.
func TestSelectBackendFollowsInventoryOrder(t *testing.T) {
	write := func(share, name, launcher string) {
		t.Helper()
		var def map[string]any
		if err := json.Unmarshal([]byte(fakeDefinition), &def); err != nil {
			t.Fatal(err)
		}
		def["name"] = name
		def["launchers"] = map[string]any{launcher: def["launchers"].(map[string]any)["checkterm"]}
		data, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(share, terminal.DefinitionsDirName)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	global, project := t.TempDir(), t.TempDir()
	write(global, "zellij", "zellij-tab")
	write(project, "zj", "zellij")
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir {
		return []terminal.DefinitionDir{
			{Source: terminal.SourceGlobal, Root: global, Path: filepath.Join(global, terminal.DefinitionsDirName)},
			{Source: terminal.SourceProject, Root: project, Path: filepath.Join(project, terminal.DefinitionsDirName)},
		}
	}
	terminal.ReloadDefinitions()
	t.Cleanup(func() { terminal.DefinitionDirs = saved; terminal.ReloadDefinitions() })

	for name, want := range map[string]string{"zellij": "zellij-tab", "zj": "zellij", "zellij-tab": "zellij-tab"} {
		backend, err := selectBackend(name)
		if err != nil || backend.Name() != want {
			t.Fatalf("select %s: backend=%v err=%v; want %s", name, backend, err, want)
		}
	}
}
