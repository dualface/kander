package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func disableRules(t *testing.T) {
	t.Helper()
	cfg := envConfig("codex", "tmux", nil)
	cfg.Rules = config.DefaultRules(false)
	loadEffective = func() (*config.Config, error) { return cfg, nil }
}

func TestDisabledGroupsDoNotClaimOrStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX Agent fakes")
	}
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata", true: "legacy"}[legacy], func(t *testing.T) {
			root, _, _ := setupBoard(t)
			disableRules(t)
			id, path := makeTodo(t, root, "disabled-group")
			text := mustRead(t, path)
			if legacy {
				text = strings.Replace(text, "## DISCUSSION", "## DISCUSSION\n\n```text\nTASK_GROUP: 20260901-disabled-group\n```", 1)
			} else {
				text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: 20260901-disabled-group", 1)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := commandStart(root, "cursor", "", id); err == nil || !strings.Contains(err.Error(), "20260901-disabled-group") {
				t.Fatalf("group should fail before Agent preparation: %v", err)
			}
			if got := mustRead(t, path); got != text {
				t.Fatal("card changed")
			}
			if _, err := os.Stat(filepath.Join(root, "tmux.log")); !os.IsNotExist(err) {
				t.Fatalf("launcher was used: %v", err)
			}
		})
	}
}
