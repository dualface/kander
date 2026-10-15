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
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	work := t.TempDir()
	t.Chdir(work)
	overlay := filepath.Join(work, config.OverlayFilename)
	if err := os.WriteFile(overlay, []byte(`{"agents":{"codex":{"config_dir":"/tmp/elsewhere"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, false, false)
	})
	for _, line := range lines {
		if line.Level == LevelWarning && strings.Contains(line.Text, "agents.codex.config_dir") &&
			strings.Contains(line.Text, config.OverlayFilename) {
			return
		}
	}
	t.Fatalf("missing overlay config_dir warning: %+v", lines)
}
