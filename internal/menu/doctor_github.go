package menu

import (
	"context"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue/ghcli"
)

const githubCLIProbeTimeout = 10 * time.Second

// reportGitHubCLI prints a read-only GitHub CLI diagnostic: availability,
// version, and the authentication state of each host. The GitHub CLI is
// optional for Kander, so a missing or unauthenticated gh never makes doctor
// unhealthy, and the probe never changes a remote, an account, or a credential.
func reportGitHubCLI() {
	ctx, cancel := context.WithTimeout(context.Background(), githubCLIProbeTimeout)
	defer cancel()
	status := ghcli.NewProvider(ghcli.Options{Timeout: githubCLIProbeTimeout}).Status(ctx)
	if !status.Available {
		hint(config.Text("menu.github_cli_not_installed"))
		return
	}
	success(config.Text("menu.github_cli_path", status.Path))
	if status.Version != "" {
		success(config.Text("menu.github_cli_version", status.Version))
	}
	if status.Error != "" {
		warning(config.Text("menu.github_cli_probe_failed", status.Error))
	}
	for _, host := range status.Hosts {
		if !host.Authenticated {
			hint(config.Text("menu.github_cli_auth_failed", host.Host))
			continue
		}
		source := host.Source
		if source == "" {
			source = "N/A"
		}
		success(config.Text("menu.github_cli_authenticated", host.Host, host.Account, source))
	}
	if len(status.Hosts) == 0 && status.Error == "" {
		hint(config.Text("menu.github_cli_not_authenticated"))
	}
	if len(status.EnvTokens) > 0 {
		hint(config.Text("menu.github_cli_env_token", strings.Join(status.EnvTokens, ", ")))
	}
}
