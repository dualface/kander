package ghcli

import (
	"context"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/issue/ghcli/ghclitest"
)

func TestParseAuthStatus(t *testing.T) {
	output := "github.com\n" +
		"  \u2713 Logged in to github.com account dualface (/home/u/.config/gh/hosts.yml)\n" +
		"  - Active account: true\n" +
		"  - Token: gho_****\n" +
		"ghe.example.com\n" +
		"  X Failed to log in to ghe.example.com account svc\n"
	hosts := parseAuthStatus(output)
	if len(hosts) != 2 {
		t.Fatalf("hosts=%+v", hosts)
	}
	if !hosts[0].Authenticated || hosts[0].Host != "github.com" || hosts[0].Account != "dualface" || hosts[0].Source != "/home/u/.config/gh/hosts.yml" {
		t.Fatalf("first host=%+v", hosts[0])
	}
	if hosts[1].Authenticated || hosts[1].Host != "ghe.example.com" || hosts[1].Account != "svc" {
		t.Fatalf("second host=%+v", hosts[1])
	}
}

func TestParseAuthStatusWithoutAccounts(t *testing.T) {
	hosts := parseAuthStatus("You are not logged into any GitHub hosts. To log in, run: gh auth login\n")
	if len(hosts) != 0 {
		t.Fatalf("hosts=%+v", hosts)
	}
}

func TestParseVersion(t *testing.T) {
	if got := parseVersion([]byte("gh version 2.46.0 (2025-12-13 Ubuntu 2.46.0-4)\nhttps://github.com/cli/cli/releases/tag/v2.46.0\n")); got != "2.46.0" {
		t.Fatalf("version=%q", got)
	}
	if got := parseVersion([]byte("unexpected output")); got != "unexpected output" {
		t.Fatalf("version=%q", got)
	}
}

func TestStatusWithoutCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	status := NewProvider(Options{}).Status(context.Background())
	if status.Available {
		t.Fatalf("status=%+v", status)
	}
	if status.Error == "" {
		t.Fatal("expected a diagnostic for a missing CLI")
	}
}

func TestStatusWithFakeCLI(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.SetRoutes(t, "gh", map[string]ghclitest.Options{
		"--version": {Stdout: "gh version 2.46.0 (2025-12-13 Ubuntu 2.46.0-4)\n"},
		"auth status": {
			Stdout: "github.com\n  \u2713 Logged in to github.com account dualface (keyring)\n",
		},
	})
	t.Setenv("GH_TOKEN", "ghp_abcdefghijklmnopqrstuvwxyz012345")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")

	status := NewProvider(Options{}).Status(context.Background())
	if !status.Available {
		t.Fatalf("status=%+v", status)
	}
	if status.Version != "2.46.0" {
		t.Fatalf("version=%q", status.Version)
	}
	if status.Path == "" {
		t.Fatal("path is empty")
	}
	if status.Error != "" {
		t.Fatalf("error=%q", status.Error)
	}
	if len(status.Hosts) != 1 || !status.Authenticated() || status.Hosts[0].Account != "dualface" || status.Hosts[0].Source != "keyring" {
		t.Fatalf("hosts=%+v", status.Hosts)
	}
	if len(status.EnvTokens) != 1 || status.EnvTokens[0] != "GH_TOKEN" {
		t.Fatalf("env tokens=%v", status.EnvTokens)
	}
	if strings.Contains(status.Error+status.Version+status.Path, "ghp_") {
		t.Fatalf("status leaks a token: %+v", status)
	}
}

func TestStatusReportsNotAuthenticatedState(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.SetRoutes(t, "gh", map[string]ghclitest.Options{
		"--version":   {Stdout: "gh version 2.46.0 (2025-12-13 Ubuntu 2.46.0-4)\n"},
		"auth status": {Stdout: "You are not logged into any GitHub hosts. To log in, run: gh auth login\n", Exit: 1},
	})
	status := NewProvider(Options{}).Status(context.Background())
	if len(status.Hosts) != 0 || status.Authenticated() {
		t.Fatalf("hosts=%+v", status.Hosts)
	}
	if status.Error != "" {
		t.Fatalf("unauthenticated state must not be a probe failure: %q", status.Error)
	}
}
