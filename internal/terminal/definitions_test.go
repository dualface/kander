package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// isolateDefinitions points the user directories at temporary share
// directories and starts from the given embedded definitions.
func isolateDefinitions(t *testing.T, embedded ...*loadedDefinition) (global, project string) {
	t.Helper()
	root := t.TempDir()
	global = filepath.Join(root, "global")
	project = filepath.Join(root, "project")
	definitionRegistry.Lock()
	saved := definitionRegistry.embedded
	definitionRegistry.embedded = embedded
	definitionRegistry.userLoaded = false
	definitionRegistry.Unlock()
	savedDirs := DefinitionDirs
	DefinitionDirs = func() []DefinitionDir {
		return []DefinitionDir{
			{Source: SourceGlobal, Root: global, Path: filepath.Join(global, DefinitionsDirName)},
			{Source: SourceProject, Root: project, Path: filepath.Join(project, DefinitionsDirName)},
		}
	}
	t.Cleanup(func() {
		DefinitionDirs = savedDirs
		definitionRegistry.Lock()
		definitionRegistry.embedded = saved
		definitionRegistry.Unlock()
		ReloadDefinitions()
	})
	return global, project
}

func writeDefinition(t *testing.T, share, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(share, DefinitionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureWith(t *testing.T, name, launcher, binary string) []byte {
	t.Helper()
	return mutateFixture(t, func(root map[string]any) {
		root["name"], root["binary"] = name, binary
		launchers := object(root, "launchers")
		spec := launchers["faketerm"]
		delete(launchers, "faketerm")
		launchers[launcher] = spec
	})
}

func embeddedFixture(t *testing.T, binary string) *loadedDefinition {
	t.Helper()
	def, err := DecodeDefinition("definitions/faketerm.json", fixtureWith(t, "faketerm", "faketerm", binary))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := newLoadedDefinition(SourceEmbedded, "definitions/faketerm.json", def)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func executableOf(t *testing.T, launcher string) string {
	t.Helper()
	backend, ok := Lookup(launcher)
	if !ok {
		t.Fatalf("launcher %s not loaded", launcher)
	}
	return backend.Executable()
}

func TestDefinitionSearchPathPrecedence(t *testing.T) {
	resetLanguage(t)
	global, project := isolateDefinitions(t, embeddedFixture(t, "embedded-term"))
	if got := executableOf(t, "faketerm"); got != "embedded-term" {
		t.Fatalf("embedded binary=%s", got)
	}

	globalPath := writeDefinition(t, global, "faketerm", fixtureWith(t, "faketerm", "faketerm", "global-term"))
	ReloadDefinitions()
	if got := executableOf(t, "faketerm"); got != "global-term" {
		t.Fatalf("global must replace embedded, binary=%s", got)
	}

	projectPath := writeDefinition(t, project, "faketerm", fixtureWith(t, "faketerm", "faketerm", "project-term"))
	ReloadDefinitions()
	if got := executableOf(t, "faketerm"); got != "project-term" {
		t.Fatalf("project must replace global, binary=%s", got)
	}
	reports := DefinitionReports()
	if len(reports) != 2 || reports[0].Path != globalPath || reports[0].Active || reports[1].Path != projectPath || !reports[1].Active {
		t.Fatalf("reports=%+v", reports)
	}

	// A corrupt project file is reported and the global definition stays.
	if err := os.WriteFile(projectPath, []byte(`{"schema_version": 1,`), 0o644); err != nil {
		t.Fatal(err)
	}
	ReloadDefinitions()
	if got := executableOf(t, "faketerm"); got != "global-term" {
		t.Fatalf("corrupt project file must fall back, binary=%s", got)
	}
	reports = DefinitionReports()
	if len(reports) != 2 || !reports[0].Active || reports[1].Err == nil || !strings.Contains(reports[1].Err.Error(), projectPath) {
		t.Fatalf("reports=%+v", reports)
	}

	// Removing both files restores the embedded definition.
	for _, path := range []string{globalPath, projectPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	ReloadDefinitions()
	if got := executableOf(t, "faketerm"); got != "embedded-term" || len(DefinitionReports()) != 0 {
		t.Fatalf("binary=%s reports=%+v", got, DefinitionReports())
	}
}

func TestUserDefinitionLaunchersReachConfigValidation(t *testing.T) {
	resetLanguage(t)
	global, _ := isolateDefinitions(t)
	if config.ValidLauncherName("zellij") {
		t.Fatal("launcher valid before its definition exists")
	}
	path := writeDefinition(t, global, "zellij", fixtureWith(t, "zellij", "zellij", "zellij"))
	ReloadDefinitions()
	if !config.ValidLauncherName("zellij") {
		t.Fatalf("launcher names=%q", config.LauncherNames())
	}
	if backend, _, ok := ParseWindow("zellij:w1:p1"); !ok || backend.Name() != "zellij" {
		t.Fatal("WINDOW of a definition launcher is not parsed")
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	ReloadDefinitions()
	if config.ValidLauncherName("zellij") || !config.ValidLauncherName("tmux") {
		t.Fatal("a damaged definition must drop only its own launcher")
	}
}

func TestDefinitionLauncherConflicts(t *testing.T) {
	resetLanguage(t)
	global, _ := isolateDefinitions(t, embeddedFixture(t, "embedded-term"))
	writeDefinition(t, global, "other", fixtureWith(t, "other", "faketerm", "other-term"))
	writeDefinition(t, global, "reserved", fixtureWith(t, "reserved", "auto", "reserved-term"))
	ReloadDefinitions()
	reports := DefinitionReports()
	if len(reports) != 2 {
		t.Fatalf("reports=%+v", reports)
	}
	for _, report := range reports {
		if report.Err == nil || report.Active {
			t.Fatalf("conflicting definition accepted: %+v", report)
		}
	}
	if !strings.Contains(reports[0].Err.Error(), "already provided by definition faketerm") || !strings.Contains(reports[1].Err.Error(), "reserved") {
		t.Fatalf("reports=%+v", reports)
	}
	if got := executableOf(t, "faketerm"); got != "embedded-term" {
		t.Fatalf("binary=%s", got)
	}
}

func TestDefinitionFilesAreReadWithoutFollowingLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	resetLanguage(t)
	global, _ := isolateDefinitions(t)
	target := filepath.Join(t.TempDir(), "zellij.json")
	if err := os.WriteFile(target, fixtureWith(t, "zellij", "zellij", "zellij"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(global, DefinitionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "zellij.json")); err != nil {
		t.Fatal(err)
	}
	ReloadDefinitions()
	reports := DefinitionReports()
	if len(reports) != 1 || reports[0].Err == nil {
		t.Fatalf("symlinked definition accepted: %+v", reports)
	}
	if _, ok := Lookup("zellij"); ok {
		t.Fatal("symlinked definition registered")
	}
}

func TestAutoResolutionOrdersDefinitionsByPriority(t *testing.T) {
	resetLanguage(t)
	global, _ := isolateDefinitions(t)
	low := mutateFixture(t, func(root map[string]any) {
		root["name"] = "low"
		launchers := object(root, "launchers")
		launchers["low"] = launchers["faketerm"]
		delete(launchers, "faketerm")
		object(root, "launchers", "low")["auto_priority"] = 5
	})
	writeDefinition(t, global, "low", low)
	writeDefinition(t, global, "zhigh", fixtureWith(t, "zhigh", "zhigh", "zhigh"))
	ReloadDefinitions()
	env := func(name string) string {
		if name == "FAKETERM" {
			return "1"
		}
		return ""
	}
	backend, ok := ResolveAuto(false, env)
	if !ok || backend.Name() != "zhigh" {
		t.Fatalf("auto=%v ok=%v", backend, ok)
	}
	if _, ok := ResolveAuto(false, func(string) string { return "" }); ok {
		t.Fatal("auto resolved without the required environment")
	}
}
