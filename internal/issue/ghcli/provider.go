package ghcli

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/issue"
)

// Environment variables GitHub CLI itself defines; Kander reads them but never
// writes, clears, or echoes their values.
const (
	EnvRepo            = "GH_REPO"
	EnvHost            = "GH_HOST"
	EnvToken           = "GH_TOKEN"
	EnvGitHubToken     = "GITHUB_TOKEN"
	EnvEnterpriseToken = "GH_ENTERPRISE_TOKEN"
	EnvEnterpriseGit   = "GITHUB_ENTERPRISE_TOKEN"
)

var environmentTokenNames = []string{EnvToken, EnvGitHubToken, EnvEnterpriseToken, EnvEnterpriseGit}

// Options configures a Provider. Zero values select production defaults.
type Options struct {
	Gh          CommandRunner
	Git         CommandRunner
	Env         func(string) string
	Timeout     time.Duration
	StdoutLimit int64
	StderrLimit int64
}

// Provider resolves canonical repository identity through the GitHub CLI.
type Provider struct {
	gh  CommandRunner
	git CommandRunner
	env func(string) string
}

// NewProvider builds a provider with the production process boundary unless a
// caller injects its own runners.
func NewProvider(options Options) *Provider {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	stdoutLimit := options.StdoutLimit
	if stdoutLimit <= 0 {
		stdoutLimit = DefaultStdoutLimit
	}
	stderrLimit := options.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = DefaultStderrLimit
	}
	gh := options.Gh
	if gh == nil {
		gh = &ExecRunner{
			Program:     "gh",
			MissingKind: issue.ErrorCLIUnavailable,
			Timeout:     timeout,
			StdoutLimit: stdoutLimit,
			StderrLimit: stderrLimit,
		}
	}
	git := options.Git
	if git == nil {
		git = &ExecRunner{
			Program:     "git",
			MissingKind: issue.ErrorGitUnavailable,
			Timeout:     timeout,
			StdoutLimit: stdoutLimit,
			StderrLimit: stderrLimit,
		}
	}
	env := options.Env
	if env == nil {
		env = os.Getenv
	}
	return &Provider{gh: gh, git: git, env: env}
}

type remoteCandidate struct {
	Name string
	Ref  issue.RepositoryRef
}

// ResolveRepository returns the canonical repository identity for an explicit
// [HOST/]OWNER/REPO reference or for the current Git worktree. Multiple
// distinct candidates are never guessed: the caller must pass an explicit
// reference or resolve the default remote with `gh repo set-default`.
func (p *Provider) ResolveRepository(ctx context.Context, directory string, explicit string) (issue.Repository, error) {
	dir, err := validatedDirectory(directory)
	if err != nil {
		return issue.Repository{}, err
	}
	ref, remote, err := p.selectReference(ctx, dir, explicit)
	if err != nil {
		return issue.Repository{}, err
	}
	return p.confirm(ctx, dir, ref, remote)
}

// selectReference applies the deterministic precedence: explicit argument,
// GH_REPO environment override, `remote.<name>.gh-resolved` default, then the
// only remote that looks like a repository.
func (p *Provider) selectReference(ctx context.Context, dir, explicit string) (issue.RepositoryRef, string, error) {
	value := strings.TrimSpace(explicit)
	if value == "" {
		value = strings.TrimSpace(p.env(EnvRepo))
	}
	if value != "" {
		ref, err := issue.ParseRepositoryRef(value)
		if err != nil {
			return issue.RepositoryRef{}, "", err
		}
		if ref.Host == "" {
			host, err := issue.NormalizeHost(strings.TrimSpace(p.env(EnvHost)))
			if err == nil {
				ref.Host = host
			} else if strings.TrimSpace(p.env(EnvHost)) != "" {
				return issue.RepositoryRef{}, "", issue.NewError(issue.ErrorInvalidReference, "host", issue.Sanitize(p.env(EnvHost)))
			}
		}
		return ref, "", nil
	}

	candidates, insecure, defaultRemote, err := p.remoteCandidates(ctx, dir)
	if err != nil {
		return issue.RepositoryRef{}, "", err
	}
	distinct := make([]remoteCandidate, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		key := strings.ToLower(candidate.Ref.String())
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		distinct = append(distinct, candidate)
	}
	if len(distinct) == 0 {
		if len(insecure) > 0 {
			return issue.RepositoryRef{}, "", &issue.Error{
				Kind:   issue.ErrorInsecureRemote,
				Op:     "remote",
				Detail: issue.Sanitize(insecure[0]),
			}
		}
		return issue.RepositoryRef{}, "", &issue.Error{Kind: issue.ErrorNoRemote, Op: "remote", Detail: issue.Sanitize(dir)}
	}
	if len(distinct) == 1 {
		return distinct[0].Ref, distinct[0].Name, nil
	}
	for _, candidate := range distinct {
		if candidate.Name == defaultRemote {
			return candidate.Ref, candidate.Name, nil
		}
	}
	labels := make([]string, 0, len(distinct))
	for _, candidate := range distinct {
		label := candidate.Ref.String() + " (" + candidate.Name + ")"
		labels = append(labels, label)
	}
	return issue.RepositoryRef{}, "", &issue.Error{Kind: issue.ErrorAmbiguousRemotes, Op: "remote", Candidates: labels}
}

// remoteCandidates reads the configured remotes through `git config --local`
// with a direct argv array. `--local` both restricts the answer to this
// worktree and turns "not a Git worktree" into a distinct failure, while an
// empty repository configuration still exits zero. A credential-bearing remote
// URL is reported as an insecure remote instead of being treated as absent.
func (p *Provider) remoteCandidates(ctx context.Context, dir string) ([]remoteCandidate, []string, string, error) {
	stdout, stderr, err := p.git.Run(ctx, dir, []string{"config", "--local", "--list"}, DefaultStdoutLimit)
	if err != nil {
		if issue.KindOf(err) == issue.ErrorCommandFailed {
			lowered := strings.ToLower(string(stderr))
			if strings.Contains(lowered, "not a git repository") || strings.Contains(lowered, "--local can only be used inside a git repository") {
				return nil, nil, "", &issue.Error{Kind: issue.ErrorNotRepository, Op: "git", Detail: issue.Sanitize(dir)}
			}
		}
		return nil, nil, "", err
	}
	var names []string
	urls := map[string][]string{}
	resolved := map[string]string{}
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.HasPrefix(key, "remote.") {
			continue
		}
		rest := strings.TrimPrefix(key, "remote.")
		switch {
		case strings.HasSuffix(rest, ".url"):
			name := strings.TrimSuffix(rest, ".url")
			if _, seen := urls[name]; !seen {
				names = append(names, name)
			}
			urls[name] = append(urls[name], value)
		case strings.HasSuffix(rest, ".gh-resolved"):
			name := strings.TrimSuffix(rest, ".gh-resolved")
			resolved[name] = strings.TrimSpace(value)
		}
	}
	defaultRemote := ""
	for _, name := range names {
		if strings.TrimSpace(resolved[name]) != "" {
			defaultRemote = name
			break
		}
	}
	candidates := make([]remoteCandidate, 0, len(names))
	var insecure []string
	for _, name := range names {
		for _, raw := range urls[name] {
			ref, ok, parseErr := issue.ParseRemoteURL(raw)
			if parseErr != nil {
				insecure = append(insecure, name)
				continue
			}
			if !ok {
				continue
			}
			candidates = append(candidates, remoteCandidate{Name: name, Ref: ref})
		}
	}
	return candidates, insecure, defaultRemote, nil
}

type repositoryView struct {
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
	IsPrivate     *bool  `json:"isPrivate"`
}

// confirm asks GitHub CLI for the canonical identity of the selected
// repository and validates the response against the requested reference.
func (p *Provider) confirm(ctx context.Context, dir string, ref issue.RepositoryRef, remote string) (issue.Repository, error) {
	reference := ref.String()
	if reference == "" {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidReference, "repo", "empty reference")
	}
	stdout, stderr, err := p.gh.Run(
		ctx, dir,
		[]string{"repo", "view", reference, "--json", "nameWithOwner,url,isPrivate"},
		DefaultStdoutLimit,
	)
	if err != nil {
		if issue.KindOf(err) == issue.ErrorCommandFailed {
			return issue.Repository{}, classifyRepositoryFailure(stderr, err, ref)
		}
		return issue.Repository{}, err
	}
	if !utf8.Valid(stdout) {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "response is not valid UTF-8")
	}
	var view repositoryView
	if err := json.Unmarshal(stdout, &view); err != nil {
		return issue.Repository{}, issue.WrapError(err, issue.ErrorInvalidResponse, "decode", issue.Sanitize(err.Error()))
	}
	return buildRepository(view, ref, remote)
}

func buildRepository(view repositoryView, ref issue.RepositoryRef, remote string) (issue.Repository, error) {
	parts := strings.Split(view.NameWithOwner, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "missing nameWithOwner")
	}
	if view.IsPrivate == nil {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "missing isPrivate")
	}
	parsed, err := url.Parse(view.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "invalid url")
	}
	host, err := issue.NormalizeHost(parsed.Hostname())
	if err != nil {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "invalid url host")
	}
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) != 2 {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "url does not name a repository")
	}
	owner, name := parts[0], parts[1]
	if !strings.EqualFold(pathParts[0], owner) || !strings.EqualFold(pathParts[1], name) {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "url does not match nameWithOwner")
	}
	if _, err := issue.ParseRepositoryRef(host + "/" + owner + "/" + name); err != nil {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "decode", "invalid canonical identity")
	}
	if ref.Host != "" && !strings.EqualFold(host, ref.Host) {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "confirm", "host does not match the request")
	}
	if !strings.EqualFold(owner, ref.Owner) || !strings.EqualFold(name, ref.Name) {
		return issue.Repository{}, issue.NewError(issue.ErrorInvalidResponse, "confirm", "repository does not match the request")
	}
	return issue.Repository{
		Host:    host,
		Owner:   owner,
		Name:    name,
		URL:     view.URL,
		Private: *view.IsPrivate,
		Remote:  remote,
	}, nil
}

// classifyRepositoryFailure maps a gh failure to a stable category. Only the
// sanitized first meaningful line is kept as the detail.
func classifyRepositoryFailure(stderr []byte, err error, ref issue.RepositoryRef) *issue.Error {
	return classifyFailure(stderr, err, ref.Host, ref.String())
}

// classifyFailure is the shared category mapping for every gh interaction. The
// caller supplies the host and the detail to show for an HTTP 404.
func classifyFailure(stderr []byte, err error, host string, notFoundDetail string) *issue.Error {
	detail := issue.Sanitize(string(stderr))
	if detail == "" {
		if structured, ok := err.(*issue.Error); ok {
			detail = structured.Detail
		}
	}
	lowered := strings.ToLower(detail)
	failed := func(kind issue.ErrorKind) *issue.Error {
		structured := issue.WrapError(err, kind, "gh", detail)
		structured.Host = host
		return structured
	}
	switch {
	case lowered == "":
		return failed(issue.ErrorCommandFailed)
	case strings.Contains(lowered, "saml") || strings.Contains(lowered, "sso"):
		return failed(issue.ErrorSSORequired)
	case strings.Contains(lowered, "rate limit") || strings.Contains(lowered, "retry-after"):
		return failed(issue.ErrorRateLimited)
	case strings.Contains(lowered, "not logged in") || strings.Contains(lowered, "gh auth login"):
		return failed(issue.ErrorUnauthenticated)
	case strings.Contains(lowered, "http 401") || strings.Contains(lowered, "authentication"):
		return failed(issue.ErrorUnauthenticated)
	case strings.Contains(lowered, "http 403") || strings.Contains(lowered, "resource not accessible") || strings.Contains(lowered, "forbidden"):
		return failed(issue.ErrorUnauthorized)
	case strings.Contains(lowered, "http 404") || strings.Contains(lowered, "could not resolve to a repository") || strings.Contains(lowered, "not found"):
		structured := failed(issue.ErrorNotFound)
		structured.Detail = notFoundDetail
		return structured
	case strings.Contains(lowered, "no default repository has been set") || strings.Contains(lowered, "multiple remotes") || strings.Contains(lowered, "repo set-default"):
		return failed(issue.ErrorAmbiguousRemotes)
	case strings.Contains(lowered, "not a git repository"):
		return failed(issue.ErrorNotRepository)
	default:
		return failed(issue.ErrorCommandFailed)
	}
}
