//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestDoctorWarnsAboutOverlayConfigDir(t *testing.T) {
	for _, value := range []string{"/tmp/elsewhere", "relative"} {
		t.Run(value, func(t *testing.T) { checkOverlayConfigDirWarning(t, value) })
	}
}

func checkOverlayConfigDirWarning(t *testing.T, value string) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	work := t.TempDir()
	t.Chdir(work)
	overlay := filepath.Join(work, config.OverlayFilename)
	if err := os.WriteFile(overlay, []byte(`{"agents":{"codex":{"config_dir":"`+value+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, false, false)
	})
	for _, line := range lines {
		if strings.Contains(line.Text, "config_dir") && !strings.Contains(line.Text, config.OverlayFilename) {
			t.Fatalf("overlay value failed validation: %+v", line)
		}
	}
	for _, line := range lines {
		if line.Level == LevelWarning && strings.Contains(line.Text, "agents.codex.config_dir") &&
			strings.Contains(line.Text, config.OverlayFilename) {
			return
		}
	}
	t.Fatalf("missing overlay config_dir warning: %+v", lines)
}
