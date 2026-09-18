package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

func TestStartupVersionRoundTrip(t *testing.T) {
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	if got := StartupVersion(paths); got != "" {
		t.Fatalf("StartupVersion on missing state = %q, want \"\"", got)
	}
	old := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = old })
	if err := RecordStartupVersion(paths); err != nil {
		t.Fatalf("RecordStartupVersion: %v", err)
	}
	if got := StartupVersion(paths); got != "1.2.3" {
		t.Fatalf("StartupVersion = %q, want 1.2.3", got)
	}
	version.Version = "1.3.0"
	if err := RecordStartupVersion(paths); err != nil {
		t.Fatalf("RecordStartupVersion again: %v", err)
	}
	if got := StartupVersion(paths); got != "1.3.0" {
		t.Fatalf("StartupVersion after rewrite = %q, want 1.3.0", got)
	}
}

func TestStartupVersionCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, startupStateFileName), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: dir}
	if got := StartupVersion(paths); got != "" {
		t.Fatalf("StartupVersion on corrupt state = %q, want \"\"", got)
	}
}
