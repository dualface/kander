//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInstallWizardListsFilesAndCancelWritesNothing drives kander install up to the rules
// confirmation and cancels there: the plan names the agent file, and nothing is written.
func TestInstallWizardListsFilesAndCancelWritesNothing(t *testing.T) {
	bin := buildKander(t)
	home, env := isolatedHome(t)
	if err := os.RemoveAll(filepath.Join(home, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	var childEnv []string
	for _, item := range childEnvWithoutKander(t, env) {
		if !strings.HasPrefix(item, "KANDER_CONFIG=") {
			childEnv = append(childEnv, item)
		}
	}
	session := startPTYAt(t, home, bin, childEnv, "install")
	if !session.waitForPlain("Language", 10*time.Second) {
		t.Fatalf("missing language step: %s", session.text())
	}
	session.send("\r")
	if !session.waitForPlain("Global (configuration and agent rules)", 10*time.Second) {
		t.Fatalf("missing scope step: %s", session.text())
	}
	session.send("\r")
	for _, want := range []string{"Kander will write these agent files", "create  ~/.claude/CLAUDE.md (Claude)"} {
		if !session.waitForPlain(want, 10*time.Second) {
			t.Fatalf("missing %q: %s", want, session.text())
		}
	}
	session.send("\x03")
	select {
	case <-session.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("install did not exit: %s", session.text())
	}
	if !strings.Contains(session.text(), "installation cancelled") {
		t.Fatalf("missing cancel notice: %s", session.text())
	}
	for _, path := range []string{
		filepath.Join(home, ".agents"),
		filepath.Join(home, ".config", "kander"),
		filepath.Join(home, ".local"),
		filepath.Join(home, ".claude", "CLAUDE.md"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cancelled install wrote %s: %v", path, err)
		}
	}
}
