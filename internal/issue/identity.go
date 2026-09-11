// Package issue holds the provider-neutral repository identity, the repository
// resolution interface, and the structured errors of Kander's GitHub issue
// integration.
//
// Identity, reference validation, issue queries, and the structured errors
// depend on neither board, launch, nor TUI, so those layers never bind to
// GitHub CLI concepts. Concrete providers such as internal/issue/ghcli
// implement the resolution interface on top of this contract. The one
// exception is the atomic issue import, which (as AGENTS.md records) depends
// one way on internal/board; provider-neutral code must never import it back.
package issue

import (
	"net/url"
	"strings"
)

const (
	maxHostLength = 253
	maxSegmentLen = 100
)

// Repository is the canonical identity of one remote repository as confirmed by
// the provider. Owner and Name keep the provider's canonical spelling.
type Repository struct {
	Host    string
	Owner   string
	Name    string
	URL     string
	Private bool
	Remote  string
}

// RepositoryRef is a validated [HOST/]OWNER/REPO reference before a provider
// confirms it. Host is empty when the reference relies on the provider default.
type RepositoryRef struct {
	Host  string
	Owner string
	Name  string
}

// String returns [HOST/]OWNER/REPO, omitting the host when it is absent.
func (r RepositoryRef) String() string {
	if r.Host == "" {
		return r.Owner + "/" + r.Name
	}
	return r.Host + "/" + r.Owner + "/" + r.Name
}

// ParseRepositoryRef validates an explicit [HOST/]OWNER/REPO reference.
func ParseRepositoryRef(value string) (RepositoryRef, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return RepositoryRef{}, NewError(ErrorInvalidReference, "repo", "")
	}
	if !printableASCII(trimmed) {
		return RepositoryRef{}, NewError(ErrorInvalidReference, "repo", Sanitize(trimmed))
	}
	parts := strings.Split(trimmed, "/")
	var ref RepositoryRef
	switch len(parts) {
	case 2:
		ref.Owner, ref.Name = parts[0], parts[1]
	case 3:
		host, err := NormalizeHost(parts[0])
		if err != nil {
			return RepositoryRef{}, referenceHostError(err, trimmed)
		}
		ref.Host = host
		ref.Owner, ref.Name = parts[1], parts[2]
	default:
		return RepositoryRef{}, NewError(ErrorInvalidReference, "repo", Sanitize(trimmed))
	}
	if err := validateOwner(ref.Owner); err != nil {
		return RepositoryRef{}, NewError(ErrorInvalidReference, "repo", Sanitize(trimmed))
	}
	name := trimRepositorySuffix(ref.Name)
	if err := validateRepositoryName(name); err != nil {
		return RepositoryRef{}, NewError(ErrorInvalidReference, "repo", Sanitize(trimmed))
	}
	ref.Name = name
	return ref, nil
}

// ParseRemoteURL interprets one Git remote URL. It reports ok=false for values
// that cannot name a repository (local paths, file URLs, unexpected shapes) and
// returns an ErrorInsecureRemote error when the URL embeds credentials. Both
// the HTTPS and the SSH forms of a GitHub Enterprise host are accepted.
func ParseRemoteURL(raw string) (RepositoryRef, bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" || !printableASCII(value) {
		return RepositoryRef{}, false, nil
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return RepositoryRef{}, false, nil
		}
		switch parsed.Scheme {
		case "http", "https", "ssh", "git", "git+ssh", "git+https":
		default:
			return RepositoryRef{}, false, nil
		}
		if parsed.User != nil {
			if password, hasPassword := parsed.User.Password(); hasPassword && password != "" {
				return RepositoryRef{}, false, NewError(ErrorInsecureRemote, "remote", "")
			}
			if strings.Contains(parsed.User.Username(), ":") {
				return RepositoryRef{}, false, NewError(ErrorInsecureRemote, "remote", "")
			}
		}
		// parsed.Host keeps an explicit port so it can be refused instead of
		// being silently dropped.
		return refFromHostPath(parsed.Host, parsed.Path)
	}
	// SCP-like syntax: [user@]host:owner/repo.git
	if at := strings.Index(value, "@"); at > 0 {
		user := value[:at]
		rest := value[at+1:]
		colon := strings.Index(rest, ":")
		if colon > 0 && !strings.Contains(rest[:colon], "/") {
			if strings.Contains(user, ":") {
				return RepositoryRef{}, false, NewError(ErrorInsecureRemote, "remote", "")
			}
			return refFromHostPath(rest[:colon], rest[colon+1:])
		}
	}
	return RepositoryRef{}, false, nil
}

func refFromHostPath(host, path string) (RepositoryRef, bool, error) {
	trimmedPath := strings.Trim(strings.TrimSpace(path), "/")
	if trimmedPath == "" {
		return RepositoryRef{}, false, nil
	}
	segments := strings.Split(trimmedPath, "/")
	if len(segments) != 2 {
		return RepositoryRef{}, false, nil
	}
	normalizedHost, err := NormalizeHost(host)
	if err != nil {
		if structured, ok := err.(*Error); ok && structured.Kind == ErrorUnsupportedHost {
			return RepositoryRef{}, false, structured
		}
		return RepositoryRef{}, false, nil
	}
	if err := validateOwner(segments[0]); err != nil {
		return RepositoryRef{}, false, nil
	}
	name := trimRepositorySuffix(segments[1])
	if err := validateRepositoryName(name); err != nil {
		return RepositoryRef{}, false, nil
	}
	return RepositoryRef{Host: normalizedHost, Owner: segments[0], Name: name}, true, nil
}

// NormalizeHost validates one hostname and lowercases it. A host with an
// explicit port is refused as ErrorUnsupportedHost instead of being accepted
// here and dropped later: `gh` cannot address such a host consistently, so the
// contract never half-accepts one.
func NormalizeHost(host string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(host))
	if value == "" || len(value) > maxHostLength {
		return "", NewError(ErrorInvalidReference, "host", "")
	}
	if strings.ContainsAny(value, " \t\r\n@/\\?#%") {
		return "", NewError(ErrorInvalidReference, "host", "")
	}
	if index := strings.LastIndex(value, ":"); index >= 0 {
		// A host written as host:port is refused instead of being accepted
		// here and silently dropped later; any other colon usage (an IPv6
		// literal, for example) is not a host Kander accepts.
		if digitsOnly(value[index+1:]) {
			return "", NewError(ErrorUnsupportedHost, "host", Sanitize(value))
		}
		return "", NewError(ErrorInvalidReference, "host", "")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return "", NewError(ErrorInvalidReference, "host", "")
		}
		for index, char := range label {
			switch {
			case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			case char == '-' && index != 0 && index != len(label)-1:
			default:
				return "", NewError(ErrorInvalidReference, "host", "")
			}
		}
	}
	return value, nil
}

// referenceHostError keeps an unsupported-host refusal distinct and reports
// every other host problem as an invalid reference.
func referenceHostError(err error, reference string) *Error {
	if structured, ok := err.(*Error); ok && structured.Kind == ErrorUnsupportedHost {
		return structured
	}
	return NewError(ErrorInvalidReference, "repo", Sanitize(reference))
}

func validateOwner(owner string) error {
	return validateSegment(owner, "owner")
}

func validateRepositoryName(name string) error {
	return validateSegment(name, "repository")
}

func validateSegment(value, kind string) error {
	if value == "" || len(value) > maxSegmentLen {
		return NewError(ErrorInvalidReference, kind, "")
	}
	if value == "." || value == ".." {
		return NewError(ErrorInvalidReference, kind, "")
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case char == '-' || char == '_' || char == '.':
		default:
			return NewError(ErrorInvalidReference, kind, "")
		}
	}
	return nil
}

func trimRepositorySuffix(name string) string {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) > 4 && strings.EqualFold(trimmed[len(trimmed)-4:], ".git") {
		return trimmed[:len(trimmed)-4]
	}
	return trimmed
}

func digitsOnly(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}

func printableASCII(value string) bool {
	for _, char := range value {
		if char < 0x20 || char > 0x7e {
			return false
		}
	}
	return true
}
