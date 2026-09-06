package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestNotifyRejectsDisabledGroupAndInvalidConfigBeforeDelivery(t *testing.T) {
	for _, invalidConfig := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled-group", true: "invalid-config"}[invalidConfig], func(t *testing.T) {
			root, _ := setupBoard(t)
			cfg := config.DefaultConfig()
			cfg.Rules = config.DefaultRules(false)
			if _, err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			id, path := makeReview(t, root, "rule-guard")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if invalidConfig {
				if err := os.WriteFile(os.Getenv(config.EnvConfig), []byte("broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				// Match the board's legacy group parser, not just the top-level field.
				data = []byte(strings.Replace(string(data), "## DISCUSSION", "## DISCUSSION\n\n```text\nTASK_GROUP: 20260901-disabled-group\n```", 1))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := commandNotify(root, id, "继续", "", "", true, 61); err == nil {
				t.Fatal("notify accepted invalid rule configuration")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(data) {
				t.Fatalf("card changed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "herdr.log.prompt")); !os.IsNotExist(err) {
				t.Fatal("message was delivered")
			}
		})
	}
}

func TestNotifyPayloadLoadsRuleConfigurationWithAllModulesOff(t *testing.T) {
	root, _ := setupBoard(t)
	cfg := config.DefaultConfig()
	cfg.Rules = config.DefaultRules(false)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	id, _ := makeReview(t, root, "rules-payload")
	_, _, err := capture(t, func() error { return commandNotify(root, id, "执行用户流程", "", "", true, 61) })
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(prompt), "请读取 ")
	if start < 0 {
		t.Fatalf("missing payload instruction: %s", prompt)
	}
	rest := string(prompt)[start+len("请读取 "):]
	end := strings.Index(rest, ", 先原样输出")
	if end < 0 {
		t.Fatalf("missing payload path: %s", prompt)
	}
	body, err := os.ReadFile(rest[:end])
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"KANDER-AGENTS.md", "config --json", "已启用", "执行用户流程"} {
		if !strings.Contains(string(body), required) {
			t.Fatalf("missing %s: %s", required, body)
		}
	}
	if strings.Contains(string(body), "提交并 push") {
		t.Fatal("disabled Git policy leaked into notification")
	}
}
