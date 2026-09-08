//go:build unix

package menu

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func initGitDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"-C", dir, "config", "user.email", "kander@example.com"},
		{"-C", dir, "config", "user.name", "Kander Test"},
		{"-C", dir, "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func writeOverlayFile(t *testing.T, dir string, payload map[string]any) (string, []byte) {
	t.Helper()
	path := filepath.Join(dir, config.OverlayFilename)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestConfigJSONMergesOverlayAndHumanPrintsPath(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"kanban_agent": "codex", "language": "cn"}))
	repo := filepath.Join(h.root, "project")
	initGitDir(t, repo)
	overlay, _ := writeOverlayFile(t, repo, map[string]any{"kanban_agent": "claude"})

	code, out, errOut := h.runIn(repo, "config", "--json")
	if code != 0 {
		t.Fatalf("json=%d %s", code, errOut)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["kanban_agent"] != "claude" {
		t.Fatalf("merged json agent=%v", payload["kanban_agent"])
	}

	code, out, errOut = h.runIn(repo, "config")
	if code != 0 {
		t.Fatalf("human=%d %s", code, errOut)
	}
	if !strings.Contains(out, overlay) || !strings.Contains(out, "项目覆盖") {
		t.Fatalf("human output missing overlay path:\n%s", out)
	}
}

func TestConfigUsesOverlayLanguage(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"kanban_agent": "codex", "language": "en"}))
	repo := filepath.Join(h.root, "project")
	initGitDir(t, repo)
	writeOverlayFile(t, repo, map[string]any{"language": "ja"})

	code, out, errOut := h.runIn(repo, "config", "--json")
	if code != 0 {
		t.Fatalf("json=%d %s", code, errOut)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["language"] != "ja" {
		t.Fatalf("merged json language=%v", payload["language"])
	}

	code, out, errOut = h.runIn(repo, "config")
	if code != 0 {
		t.Fatalf("human=%d %s", code, errOut)
	}
	if !strings.Contains(out, "プロジェクト上書き") || !strings.Contains(out, "言語") {
		t.Fatalf("human output did not use overlay language:\n%s", out)
	}
	if strings.Contains(out, "项目覆盖") || strings.Contains(out, "Language:") {
		t.Fatalf("human output still used scope or English labels:\n%s", out)
	}
}

func TestSessionSaveLeavesOverlayLanguageIsolated(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	t.Setenv(config.EnvConfig, h.configPath)
	h.writeConfig(defaultPayload(map[string]any{"kanban_agent": "codex", "language": "en"}))
	repo := filepath.Join(h.root, "project")
	initGitDir(t, repo)
	overlay, original := writeOverlayFile(t, repo, map[string]any{"language": "ja"})
	t.Chdir(repo)
	t.Setenv("PATH", h.fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	scope, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(scope, true)
	if err != nil {
		t.Fatal(err)
	}
	if session.Config.Language != "en" {
		t.Fatalf("session language=%s", session.Config.Language)
	}
	if config.ConfiguredLanguage() != "ja" {
		t.Fatalf("effective language=%s", config.ConfiguredLanguage())
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(overlay)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("overlay bytes changed: %s", data)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Language != "en" {
		t.Fatalf("TUI session save wrote overlay language into the scope file: %s", scopeCfg.Language)
	}
	merged, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Language != "ja" {
		t.Fatalf("runtime merge lost overlay language: %s", merged.Language)
	}
}

func TestDoctorRepairDoesNotWriteOverlayValues(t *testing.T) {
	h := newHarness(t)
	h.fakeCommand("claude", "")
	h.writeConfig(defaultPayload(map[string]any{
		"kanban_agent": "grok",
		"language":     "en",
		"reviewers":    map[string]any{"PM": "grok", "CSA": "grok", "Hacker": "grok", "QA": "grok"},
	}))
	repo := filepath.Join(h.root, "project")
	initGitDir(t, repo)
	overlay, original := writeOverlayFile(t, repo, map[string]any{"kanban_agent": "cursor", "language": "ja"})

	_, _, _ = h.runIn(repo, "doctor")
	data, err := os.ReadFile(overlay)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("doctor changed overlay: %s", data)
	}
	cfg := readDoctorConfig(t, h)
	if cfg.KanbanAgent == "cursor" || cfg.Language == "ja" {
		t.Fatalf("scope file absorbed overlay-only values: agent=%s language=%s", cfg.KanbanAgent, cfg.Language)
	}
}
