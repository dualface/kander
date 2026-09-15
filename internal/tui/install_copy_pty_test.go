//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupOptionalCopyOnPTY(t *testing.T) {
	bin := buildKander(t)
	for _, choice := range []string{"decline", "accept", "matched"} {
		t.Run(choice, func(t *testing.T) {
			_, env := boardEnv(t)
			writeCompleteConfig(t, env)
			var home string
			filtered := []string{}
			for _, item := range env {
				if value, ok := strings.CutPrefix(item, "HOME="); ok {
					home = value
				}
				if !strings.HasPrefix(item, "PATH=") && !strings.HasPrefix(item, "KANDER_SKIP_INSTALL=") {
					filtered = append(filtered, item)
				}
			}
			path := t.TempDir()
			if choice == "matched" {
				if err := os.Symlink(bin, filepath.Join(path, "kander")); err != nil {
					t.Fatal(err)
				}
			}
			filtered = append(filtered, "PATH="+path)
			session := startPTYAt(t, t.TempDir(), bin, filtered)
			if choice != "matched" {
				if !session.waitForPlain("Copy this executable for global use?", 10*time.Second) {
					t.Fatalf("missing copy prompt: %s", session.text())
				}
				if choice == "accept" {
					session.send("y\n")
				} else {
					session.send("n\n")
				}
			}
			if !session.waitFor("Task Board", 12*time.Second) {
				t.Fatalf("board did not open: %s", session.text())
			}
			dest := filepath.Join(home, ".local", "bin", "kander")
			_, err := os.Stat(dest)
			if choice == "accept" {
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(session.text(), "not the first kander on PATH") {
					t.Fatalf("missing PATH warning: %s", session.text())
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("unexpected copied binary: %v", err)
			}
			if choice == "matched" && strings.Contains(session.text(), "Copy this executable") {
				t.Fatal("prompted for matching symlink")
			}
		})
	}
}
