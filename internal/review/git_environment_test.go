package review

import (
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewGitSafeDirectoryEnvironment(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "O'Brien = sandbox"))
	for _, tc := range []struct {
		name, goos, prior string
		groups            []string
		add               bool
	}{
		{name: "new entry", goos: "windows", groups: []string{"CodexSandboxUsers"}, add: true},
		{name: "append", goos: "windows", groups: []string{"CodexSandboxUsers"}, prior: "'safe.directory=/existing'", add: true},
		{name: "no declared groups", goos: "windows", prior: "'safe.directory=/existing'"},
		{name: "POSIX", goos: "linux", groups: []string{"CodexSandboxUsers"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			if tc.prior != "" {
				env[gitConfigParameters] = tc.prior
			}
			before := maps.Clone(env)
			addReviewGitSafeDirectory(tc.goos, tc.groups, path, env)
			if !tc.add {
				if !maps.Equal(env, before) {
					t.Errorf("environment changed: %v", env)
				}
				return
			}

			cmd := exec.CommandContext(t.Context(), "git", "config", "--get-all", "safe.directory")
			// Scope the probe to the parameters under test rather than the user's
			// system, global, or checkout configuration.
			cmd.Dir = t.TempDir()
			cmd.Env = append(cmd.Environ(), "GIT_CONFIG_SYSTEM=", "GIT_CONFIG_GLOBAL=", "GIT_CONFIG_NOSYSTEM=1",
				"GIT_CONFIG_COUNT=0", "GIT_CEILING_DIRECTORIES="+filepath.Dir(cmd.Dir),
				gitConfigParameters+"="+env[gitConfigParameters])
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git could not read constructed environment: %v\n%s", err, out)
			}
			want := path + "\n"
			if tc.prior != "" {
				want = "/existing\n" + want
			}
			if got := strings.ReplaceAll(string(out), "\r\n", "\n"); got != want {
				t.Errorf("Git read %q, want %q (parameters %q)", got, want, env[gitConfigParameters])
			}
		})
	}
}
