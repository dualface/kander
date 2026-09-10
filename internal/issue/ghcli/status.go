package ghcli

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/issue"
)

// HostStatus is one GitHub host's read-only authentication state.
type HostStatus struct {
	Host          string
	Authenticated bool
	Account       string
	Source        string
}

// Status is the read-only GitHub CLI diagnostic used by doctor. Probing never
// changes a remote, an active account, or a credential, and the result never
// carries a token.
type Status struct {
	Available bool
	Path      string
	Version   string
	EnvTokens []string
	Hosts     []HostStatus
	Error     string
}

// Authenticated reports whether at least one host is authenticated.
func (s Status) Authenticated() bool {
	for _, host := range s.Hosts {
		if host.Authenticated {
			return true
		}
	}
	return false
}

var (
	loggedInPattern = regexp.MustCompile(`Logged in to (\S+) (?:account|as) (\S+)(?:\s+\(([^)]*)\))?`)
	failedPattern   = regexp.MustCompile(`Failed to log in to (\S+)(?:\s+(?:account|as)\s+(\S+))?`)
)

// Status probes the GitHub CLI without changing any state.
func (p *Provider) Status(ctx context.Context) Status {
	var status Status
	path, err := resolveProgram("gh")
	if err != nil {
		status.Error = issue.Sanitize(err.Error())
		return status
	}
	status.Available = true
	status.Path = path
	for _, name := range environmentTokenNames {
		if strings.TrimSpace(p.env(name)) != "" {
			status.EnvTokens = append(status.EnvTokens, name)
		}
	}
	if stdout, _, versionErr := p.gh.Run(ctx, ".", []string{"--version"}, DefaultStdoutLimit); versionErr == nil {
		status.Version = parseVersion(stdout)
	} else if status.Error == "" {
		status.Error = probeErrorText(versionErr)
	}
	stdout, stderr, authErr := p.gh.Run(ctx, ".", []string{"auth", "status"}, DefaultStdoutLimit)
	combined := string(stdout)
	if strings.TrimSpace(combined) == "" {
		combined = string(stderr)
	}
	status.Hosts = parseAuthStatus(combined)
	if authErr != nil && len(status.Hosts) == 0 && !strings.Contains(strings.ToLower(combined), "not logged into") && status.Error == "" {
		status.Error = probeErrorText(authErr)
	}
	return status
}

func parseVersion(stdout []byte) string {
	line := strings.TrimSpace(strings.SplitN(string(stdout), "\n", 2)[0])
	fields := strings.Fields(line)
	if len(fields) >= 3 && fields[0] == "gh" && fields[2] != "" {
		return fields[2]
	}
	return issue.Sanitize(line)
}

func parseAuthStatus(output string) []HostStatus {
	var hosts []HostStatus
	seen := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		if match := loggedInPattern.FindStringSubmatch(line); match != nil {
			key := strings.ToLower(match[1]) + "\x00" + match[2]
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hosts = append(hosts, HostStatus{
				Host:          match[1],
				Authenticated: true,
				Account:       match[2],
				Source:        strings.TrimSpace(match[3]),
			})
			continue
		}
		if match := failedPattern.FindStringSubmatch(line); match != nil {
			key := strings.ToLower(match[1]) + "\x00" + match[2]
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hosts = append(hosts, HostStatus{Host: match[1], Account: match[2]})
		}
	}
	return hosts
}

func probeErrorText(err error) string {
	var structured *issue.Error
	if errors.As(err, &structured) && structured.Detail != "" {
		return structured.Detail
	}
	return issue.Sanitize(err.Error())
}
