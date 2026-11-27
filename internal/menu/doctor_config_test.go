//go:build unix

package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestOperationalCommandsRequireCompleteConfig(t *testing.T) {
	commands := [][]string{
		{"config"},
		{"config", "--json"},
		{"new", "chore", "cfg-missing", "title"},
		{"new", "--language", "de", "chore", "cfg-lang", "title"},
		{"start", "20260909-cfg-task"},
		{"resume", "--message", "go", "20260909-cfg-task"},
		{"notify", "--message", "go", "20260909-cfg-task"},
		{"dismiss", "20260909-cfg-task"},
		{"review", "/tmp", strings.Repeat("a", 40), strings.Repeat("a", 40), "PM", "goal"},
		{"review", "progress", "/tmp", "20260909-cfg-task"},
		{"check"},
		{"subscribe", "20260909-cfg-group", "20260909-cfg-task"},
		{"coordinator", "show", "20260909-cfg-group"},
		{"dispatch", "show", "20260909-cfg-task", "disp-1"},
		{"dispatch", "fail", "20260909-cfg-task", "disp-1", "1", "reason"},
		{"dispatch", "cancel", "20260909-cfg-task", "disp-1", "1", "reason"},
	}
	for _, body := range []string{"", "{broken", `{"language":"xx"}`} {
		name := "missing"
		if body == "{broken" {
			name = "invalid-json"
		} else if body != "" {
			name = "invalid-schema"
		}
		t.Run(name, func(t *testing.T) {
			for _, args := range commands {
				t.Run(strings.Join(args, "_"), func(t *testing.T) {
					h := newHarness(t)
					if body != "" {
						if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(h.configPath, []byte(body), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					code, out, err := h.run(args...)
					if code == 0 {
						t.Fatalf("code=0 out=%s err=%s", out, err)
					}
					combined := out + err
					switch body {
					case "":
						if !strings.Contains(combined, "配置不存在") {
							t.Fatalf("missing config: code=%d out=%q err=%q", code, out, err)
						}
					case "{broken":
						if !strings.Contains(combined, "读取配置失败") {
							t.Fatalf("invalid JSON: code=%d out=%q err=%q", code, out, err)
						}
					default:
						if !strings.Contains(combined, "schema_version") && !strings.Contains(combined, "必须是") && !strings.Contains(combined, "language") {
							t.Fatalf("invalid schema: code=%d out=%q err=%q", code, out, err)
						}
					}
				})
			}
		})
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
	h.writeConfig(defaultPayload(map[string]any{"welcome_complete": false}))
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

func TestConfigPrintsAgentLanguage(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"language": "en", "agent_language": "ja"}))
	code, out, err := h.run("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, "agent language: ja") {
		t.Fatalf("%s", out)
	}
}
