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
	for name, config := range map[string]func(dir string) string{
		"commit signing": func(string) string {
			return "[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /nonexistent/bogus-gpg\n"
		},
		"global hook and default branch": func(dir string) string {
			hooks := filepath.Join(dir, "hooks")
			if err := os.Mkdir(hooks, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			return "[core]\n\thooksPath = " + filepath.ToSlash(hooks) + "\n[init]\n\tdefaultBranch = trunk\n"
		},
		"default ignore file": func(dir string) string {
			ignore := filepath.Join(dir, "ignore")
			if err := os.WriteFile(ignore, []byte("work.txt\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "[core]\n\texcludesFile = " + filepath.ToSlash(ignore) + "\n"
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "hostile.gitconfig")
			if err := os.WriteFile(path, []byte(config(dir)), 0o600); err != nil {
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
