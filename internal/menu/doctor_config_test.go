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

func TestDoctorRepairsSmallTaskAgentUnavailable(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("grok", "#!/bin/sh\nexit 1\n")
	h.writeConfig(defaultPayload(map[string]any{
		"kanban_agents": map[string]any{"large": "codex", "small": "grok"},
	}))
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("code=%d %s", code, err)
	}
	if cfg := readDoctorConfig(t, h); cfg.KanbanAgents["small"] != "codex" || cfg.KanbanAgents["large"] != "codex" {
		t.Fatalf("task agents not repaired: %v", cfg.KanbanAgents)
	}
}

func TestDoctorFailsWithoutAgents(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{
		"kanban_agent":  "grok",
		"kanban_agents": map[string]any{"large": "grok", "small": "cursor"},
	}))
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("code=%d %s", code, err)
	}
	if !strings.Contains(err, "没有发现可执行 Agent") || !strings.Contains(err, "没有发现 Reviewer") {
		t.Fatalf("%s", err)
	}
	if cfg := readDoctorConfig(t, h); cfg.KanbanAgents["small"] != "cursor" || cfg.KanbanAgent != "grok" {
		t.Fatal("must preserve choices when no usable replacement exists")
	}
}

func TestInvalidConfigReported(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, err := h.run("config")
	if code != 1 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "读取配置失败") {
		t.Fatalf("%s", err)
	}
}

func TestConfigHumanAndJSON(t *testing.T) {
	h := newHarness(t)
	code, out, err := h.run("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, "初始化: 未完成") || !strings.Contains(out, "看板 Agent: codex") {
		t.Fatalf("%s", out)
	}
	code, out, err = h.run("config", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	var cfg map[string]any
	if e := json.Unmarshal([]byte(out), &cfg); e != nil {
		t.Fatal(e)
	}
	if cfg["welcome_complete"] != false {
		t.Fatalf("%v", cfg)
	}
}

func TestConfigLanguageFromFile(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en"}))
	h.setenv("KANDER_LANG", "cn")
	code, out, err := h.run("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, "initialized: complete") || !strings.Contains(out, "language: English") {
		t.Fatalf("%s", out)
	}
}

func TestCLILangOverridesConfigLanguage(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en"}))
	h.setenv("KANDER_LANG", "en")
	code, out, err := h.run("--lang", "cn", "config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, "初始化: 已完成") || !strings.Contains(out, "默认语言: 英文") {
		t.Fatalf("%s", out)
	}
}

func TestDoctorAutoLauncher(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "auto"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "launcher=auto 在启动时按当前环境选择") {
		t.Fatalf("%s", err)
	}
}

func TestDoctorHerdrLauncher(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("herdr", "")
	h.writeConfig(defaultPayload(map[string]any{"launcher": "herdr", "language": "cn"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "launcher=herdr 需要当前处于 herdr") {
		t.Fatalf("%s", err)
	}
	h.setenv("HERDR_ENV", "1")
	h.setenv("HERDR_WORKSPACE_ID", "w1")
	code, _, err = h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "会在当前 workspace 新建 tab") {
		t.Fatalf("%s", err)
	}
}

func TestDoctorCursorAgent(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("cursor-agent", "")
	code, _, err := h.run("doctor")
	if !strings.Contains(err, "Cursor: cursor-agent test-version") {
		t.Fatalf("%d %s", code, err)
	}
}

func TestDoctorVersionFailure(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("codex", "#!/bin/sh\nexit 1\n")
	code, _, err := h.run("doctor")
	if code != 1 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "--version 失败") {
		t.Fatalf("%s", err)
	}
}

func TestProjectConfigAndDoctor(t *testing.T) {
	h := newHarness(t)
	project := filepath.Join(h.root, "app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q", project)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	for _, args := range [][]string{
		{"-C", project, "config", "user.email", "kander@example.com"},
		{"-C", project, "config", "user.name", "Kander Test"},
		{"-C", project, "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Kander", "GIT_AUTHOR_EMAIL=kander@example.com", "GIT_COMMITTER_NAME=Kander", "GIT_COMMITTER_EMAIL=kander@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	binDir := filepath.Join(project, ".kander", "bin")
	rulesDir := filepath.Join(project, ".kander", "rules")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(testKander, filepath.Join(binDir, "kander"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "KANDER-AGENTS.md"), []byte("# Kander 全局工作流规则\n\n项目规则入口\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalCfg := filepath.Join(h.home, ".config", "kander", "config.json")
	h.configPath = globalCfg
	h.writeConfig(defaultPayload(map[string]any{"kanban_agent": "grok"}))
	projectBin := filepath.Join(binDir, "kander")
	env := envWith(h.home, "", h.fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin:"+os.Getenv("PATH"))
	filtered := env[:0]
	for _, item := range env {
		if !strings.HasPrefix(item, "KANDER_CONFIG=") {
			filtered = append(filtered, item)
		}
	}
	env = filtered
	runProject := func(args ...string) (int, string, string) {
		cmd := exec.Command(projectBin, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return code, stdout.String(), stderr.String()
	}
	code, out, err := runProject("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, "初始化: 未完成") || !strings.Contains(out, "看板 Agent: codex") {
		t.Fatalf("%s", out)
	}
	projectCfg := filepath.Join(project, ".kander", "config.json")
	data, _ := json.Marshal(defaultPayload(map[string]any{"kanban_agent": "claude"}))
	if e := os.MkdirAll(filepath.Dir(projectCfg), 0o755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(projectCfg, data, 0o600); e != nil {
		t.Fatal(e)
	}
	h.fakeCommand("codex", "")
	h.fakeCommand("claude", "")
	h.fakeCommand("grok", "")
	h.fakeCommand("tmux", "")
	code, _, err = runProject("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	installRoot, _ := filepath.EvalSymlinks(filepath.Join(project, ".kander"))
	if !strings.Contains(err, "安装模式: 项目") || !strings.Contains(err, installRoot) {
		t.Fatalf("%s", err)
	}
	if !strings.Contains(err, filepath.Join(installRoot, "rules", "KANDER-AGENTS.md")) {
		t.Fatalf("%s", err)
	}
	if strings.Contains(err, filepath.Join(h.home, ".local", "bin")) || strings.Contains(err, ".agents") {
		t.Fatalf("leaked global path: %s", err)
	}
	if strings.Contains(err, ".config/kander") && strings.Contains(err, h.home) {
		// project config path should be used
		if !strings.Contains(err, projectCfg) && !strings.Contains(err, filepath.Join(installRoot, "config.json")) {
			t.Fatalf("%s", err)
		}
	}
}

func TestRulesIntegrationRejectsGlobalEntry(t *testing.T) {
	h := newHarness(t)
	t.Setenv("HOME", h.home)
	project := filepath.Join(h.root, "app")
	rulesDir := filepath.Join(project, ".kander", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(rulesDir, "KANDER-AGENTS.md")
	if err := os.WriteFile(entry, []byte("# project Kander entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalEntry := filepath.Join(h.home, ".agents", "KANDER-AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(globalEntry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalEntry, []byte("# global Kander entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := config.InstallPaths{
		Mode: config.ModeProject, RulesDir: rulesDir, BinDir: filepath.Join(project, ".kander", "bin"),
		ShareDir: filepath.Join(project, ".kander", "share"), ProjectRoot: project, InstallRoot: filepath.Join(project, ".kander"),
		ConfigPath: filepath.Join(project, ".kander", "config.json"),
	}
	claude := filepath.Join(project, "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(claude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte("@~/.agents/KANDER-AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _ := rulesIntegration("claude", paths)
	if ok {
		t.Fatal("expected reject global")
	}
	if err := os.WriteFile(claude, []byte("@"+entry+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, detail := rulesIntegration("claude", paths)
	if !ok {
		t.Fatalf("expected accept %s", detail)
	}
}

func TestDoctorTmuxSessionOutsideTmux(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "tmux-session"}))
	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(err, "按项目自动新建或复用专属 session") {
		t.Fatalf("%s", err)
	}
	if strings.Contains(err, "需要先进入") || strings.Contains(err, "已安装但当前不在 session") {
		t.Fatalf("%s", err)
	}
}
