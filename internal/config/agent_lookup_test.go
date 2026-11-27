package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// isolateLookup gives the test a scope config, and a process directory outside
// any project whose overlay sets no agent, as the background worker has.
func isolateLookup(t *testing.T) {
	t.Helper()
	writeScopeFile(t, filepath.Join(t.TempDir(), "config.json"))
	t.Chdir(t.TempDir())
}

func writeAgentOverlay(t *testing.T, dir string, agent map[string]any) {
	t.Helper()
	writeJSONFile(t, filepath.Join(dir, OverlayFilename), map[string]any{"agents": map[string]any{"codex": agent}})
}

func lookupProcessName(t *testing.T, lookup AgentLookup) string {
	t.Helper()
	definition, err := lookup.Agent("codex")
	if err != nil {
		t.Fatal(err)
	}
	return definition.ProcessName
}

func TestAgentLookupInReadsTheOverlayOfTheMainWorktree(t *testing.T) {
	isolateLookup(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "project"))
	writeAgentOverlay(t, project, map[string]any{"process_name": "codex-project"})
	worktree := filepath.Join(t.TempDir(), "task")
	runGit(t, project, "-C", project, "worktree", "add", "-q", worktree)
	for _, dir := range []string{project, worktree} {
		if got := lookupProcessName(t, AgentLookupIn(dir)); got != "codex-project" {
			t.Fatalf("process name read from %s = %q, want the project overlay", dir, got)
		}
	}
	if got := lookupProcessName(t, nil); got != "codex" {
		t.Fatalf("the nil lookup read %q, want the scope configuration of the process directory", got)
	}
}

func TestAgentLookupInKeepsTheProcessDirectoryForAnUnusableDirectory(t *testing.T) {
	isolateLookup(t)
	parent := t.TempDir()
	writeAgentOverlay(t, parent, map[string]any{"process_name": "codex-parent"})
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", filepath.Join(parent, "missing", "deeper"), file, "relative/dir"} {
		if got := lookupProcessName(t, AgentLookupIn(dir)); got != "codex" {
			t.Fatalf("directory %q read %q: an overlay above an unusable directory was used", dir, got)
		}
	}
}

func TestAgentLookupInRefusesAnInvalidOverlay(t *testing.T) {
	isolateLookup(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "project"))
	if err := os.WriteFile(filepath.Join(project, OverlayFilename), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AgentLookupIn(project).Agent("codex"); err == nil {
		t.Fatal("an invalid project overlay fell back to the scope configuration")
	}
}

func TestAgentLookupInReadsTheConfigurationOnce(t *testing.T) {
	isolateLookup(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "project"))
	writeAgentOverlay(t, project, map[string]any{"process_name": "codex-first"})
	lookup := AgentLookupIn(project)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _, _ = lookup.Agent("codex") })
	}
	wg.Wait()
	writeAgentOverlay(t, project, map[string]any{"process_name": "codex-second"})
	if got := lookupProcessName(t, lookup); got != "codex-first" {
		t.Fatalf("one lookup read the configuration again: %q", got)
	}
	if got := lookupProcessName(t, AgentLookupIn(project)); got != "codex-second" {
		t.Fatalf("a new lookup did not read the changed overlay: %q", got)
	}
}
