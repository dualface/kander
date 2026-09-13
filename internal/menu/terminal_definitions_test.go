package menu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

const menuDefinition = `{
  "schema_version": 1,
  "name": "zellij",
  "binary": "zellij",
  "capabilities": {"container": true, "pane_metadata": true, "foreground_process": true},
  "address": [{"name": "container"}, {"name": "pane"}],
  "errors": {},
  "launchers": {"zellij": {"requires": {"platform": "any", "binary": true, "inside_session": false}}},
  "ops": {
    "create_container": {"steps": [{"store": "new", "argv": ["new"], "fields": {"window": "regex:^(\\S+)", "pane": "regex:(\\S+)$"}}], "result": {"container": "{step.new.window}", "pane": "{step.new.pane}"}},
    "run_command": {"steps": [{"argv": ["run", "{pane}", "{command}"]}]},
    "set_session_marker": {"steps": [{"argv": ["meta", "{pane}", "{value}"]}]},
    "pane_facts": {"steps": [{"argv": ["facts", "{pane}"]}]},
    "read_output": {"steps": [{"argv": ["read", "{pane}"]}]},
    "deliver_text": {"steps": [{"argv": ["type", "{pane}", "{text}"]}]},
    "topology": {"steps": [{"argv": ["topology", "{pane}"]}]},
    "container_exists": {"steps": [{"argv": ["exists", "{container}"]}]},
    "reverse_lookup": {"steps": [{"store": "list", "argv": ["list"]}], "rows": {"from": "list", "fields": {"pane": "raw"}, "match": "field:row.pane={reference}", "result": {"container": "{row.pane}", "pane": "{row.pane}"}}},
    "close_container": {"steps": [{"argv": ["close", "{container}"]}]}
  }
}`

// A launcher from a user definition is offered in the launcher list and kept
// by doctor repair only while its program is on PATH.
func TestDefinitionLauncherChoicesAndRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake terminal program is a POSIX executable")
	}
	share := t.TempDir()
	dir := filepath.Join(share, terminal.DefinitionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zellij.json"), []byte(menuDefinition), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir {
		return []terminal.DefinitionDir{{Source: terminal.SourceGlobal, Root: share, Path: dir}}
	}
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = saved
		terminal.ReloadDefinitions()
	})

	t.Setenv("PATH", t.TempDir())
	if doctorLauncherAvailable("zellij", TerminalTools{}) {
		t.Fatal("definition launcher kept without its program")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "zellij"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if !doctorLauncherAvailable("zellij", TerminalTools{}) {
		t.Fatal("definition launcher replaced although its program is on PATH")
	}
	session := &Session{Config: config.DefaultConfig()}
	found := false
	for _, choice := range session.LauncherChoices() {
		if choice.Value == "zellij" {
			found = choice.Label == config.Text("terminal.launcher_choice", "zellij")
		}
	}
	if !found {
		t.Fatalf("launcher choices=%+v", session.LauncherChoices())
	}
	if _, ok := definitionLauncher("tmux"); ok {
		t.Fatal("the built-in tmux definition keeps its tool-specific choices")
	}
}
