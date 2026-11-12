package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceExecutableInstallAndUpdateBoundaries(t *testing.T) {
	name := "kander-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X github.com/dualface/kander/internal/version.Version=2.0.0-alpha.2", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	// Keep component administration independent of a developer's client setup.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	run := func(exe string, want int, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), exe, args...)
		out, err := cmd.CombinedOutput()
		got := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				got = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if got != want {
			t.Fatalf("%v: exit %d want %d: %s", args, got, want, out)
		}
		return string(out)
	}
	if out := run(binary, 0, "--version"); strings.TrimSpace(out) != "2.0.0-alpha.2" {
		t.Fatalf("version=%q", out)
	}
	dir := filepath.Join(t.TempDir(), "bin")
	out := run(binary, 0, "install", "--dir", dir)
	if !strings.Contains(out, `"copied":true`) {
		t.Fatalf("installation=%s", out)
	}
	installed := filepath.Join(dir, name)
	if out := run(installed, 0, "--version"); strings.TrimSpace(out) != "2.0.0-alpha.2" {
		t.Fatalf("installed version=%q", out)
	}
	if out := run(installed, 0, "update", "--check"); strings.TrimSpace(out) != "null" {
		t.Fatalf("alpha stable offer=%q", out)
	}
	run(installed, 1, "update", "--version", "1.9.0")
	run(installed, 1, "update", "--version", "2.0.0-alpha.1")
	run(installed, 1, "update", "--version", "2.0.0-alpha.2/../../invalid")
	run(installed, 2, "update", "--check", "--version", "2.1.0")
	run(installed, 2, "maintain")
	for _, args := range [][]string{{"--help"}, {"install", "--help"}, {"update", "--help"}, {"maintain", "--help"}} {
		run(installed, 0, args...)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("service wrote agent files: %v", err)
	}
}
