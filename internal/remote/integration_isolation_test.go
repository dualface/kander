package remote_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIntegrationFixtureIgnoresHostileUserGitConfig guards the order of the
// fixture: its repositories and commits must not read the developer's own Git
// configuration, whatever that configuration asks Git to do.
func TestIntegrationFixtureIgnoresHostileUserGitConfig(t *testing.T) {
	for name, hostile := range map[string]func(t *testing.T, dir string) string{
		"commit signing": func(*testing.T, string) string {
			return "[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /nonexistent/bogus-gpg\n"
		},
		"global hook and default branch": func(t *testing.T, dir string) string {
			hooks := filepath.Join(dir, "hooks")
			if err := os.Mkdir(hooks, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			return "[core]\n\thooksPath = " + filepath.ToSlash(hooks) + "\n[init]\n\tdefaultBranch = trunk\n"
		},
		// Git reads the default ignore file below XDG_CONFIG_HOME when no
		// configuration names one, so the file is placed there.
		"default ignore file": func(t *testing.T, dir string) string {
			home := filepath.Join(dir, "xdg")
			if err := os.MkdirAll(filepath.Join(home, "git"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "git", "ignore"), []byte("work.txt\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_CONFIG_HOME", home)
			return ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "hostile.gitconfig")
			if err := os.WriteFile(path, []byte(hostile(t, dir)), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", path)
			f := newIntegrationFixture(t)
			if f.base == "" || f.commit == f.base {
				t.Fatalf("the fixture did not commit: base %q, delivery %q", f.base, f.commit)
			}
		})
	}
}
