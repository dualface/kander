//go:build unix

package menu

import (
	"strings"
	"testing"
)

const fakeGitHubCLI = `#!/bin/sh
case "$1" in
  --version)
    printf 'gh version 2.46.0 (2025-12-13 Ubuntu 2.46.0-4)\n'
    ;;
  auth)
    printf 'github.com\n'
    printf '  \342\234\223 Logged in to github.com account dualface (/tmp/gh/hosts.yml)\n'
    ;;
esac
`

func TestDoctorReportsGitHubCLI(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("gh", fakeGitHubCLI)
	h.writeConfig(defaultPayload(nil))
	h.setenv("GH_TOKEN", "")
	h.setenv("GITHUB_TOKEN", "")
	h.setenv("GH_ENTERPRISE_TOKEN", "")
	h.setenv("GITHUB_ENTERPRISE_TOKEN", "")

	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	for _, want := range []string{"GitHub CLI", "2.46.0", "dualface", "github.com"} {
		if !strings.Contains(err, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, err)
		}
	}
}

func TestDoctorTreatsGitHubCLIAsOptional(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.writeConfig(defaultPayload(nil))
	h.setenv("GH_TOKEN", "")
	h.setenv("GITHUB_TOKEN", "")
	h.setenv("GH_ENTERPRISE_TOKEN", "")
	h.setenv("GITHUB_ENTERPRISE_TOKEN", "")

	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("a missing optional GitHub CLI must not make doctor unhealthy: code=%d stderr=%s", code, err)
	}
	if !strings.Contains(err, "GitHub CLI") {
		t.Fatalf("doctor output missing the GitHub CLI line:\n%s", err)
	}
}

const hostileGitHubCLI = `#!/bin/sh
case "$1" in
  --version)
    printf 'gh version \033]0;pwn\007 1.0\n'
    ;;
  auth)
    printf 'Logged in to \033[2Jevil.example.com account \033[31mroot\007 (key\033[2Jring)\n'
    ;;
esac
`

func TestDoctorSanitizesHostileGitHubCLIOutput(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("gh", hostileGitHubCLI)
	h.writeConfig(defaultPayload(nil))
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		h.setenv(name, "")
	}

	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if strings.ContainsAny(err, "\x1b\x07") {
		t.Fatalf("doctor printed a terminal escape from gh output: %q", err)
	}
	if !strings.Contains(err, "evil.example.com") || !strings.Contains(err, "root") {
		t.Fatalf("doctor dropped the real gh values:\n%s", err)
	}
}

func TestDoctorReportsGitHubCLIEnvironmentTokens(t *testing.T) {
	h := newHarness(t)
	h.installFake(true)
	h.fakeCommand("gh", fakeGitHubCLI)
	h.setenv("GH_TOKEN", "ghp_abcdefghijklmnopqrstuvwxyz012345")
	h.writeConfig(defaultPayload(nil))

	code, _, err := h.run("doctor")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if !strings.Contains(err, "GH_TOKEN") {
		t.Fatalf("doctor output missing the environment token hint:\n%s", err)
	}
	if strings.Contains(err, "ghp_abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("doctor output leaks a token value:\n%s", err)
	}
}
