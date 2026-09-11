package issue

import (
	"net/url"
	"strconv"
	"strings"
)

// Validate reports whether one resolved repository identity is internally
// consistent and safe to build links from. The canonical URL must be an HTTPS
// URL without credentials whose host and path match the identity fields; a
// provider response that fails this check is never trusted for display or for
// opening a browser.
func (r Repository) Validate() error {
	host, err := NormalizeHost(r.Host)
	if err != nil || host != r.Host {
		return NewError(ErrorInvalidReference, "host", Sanitize(r.Host))
	}
	if err := validateOwner(r.Owner); err != nil {
		return NewError(ErrorInvalidReference, "owner", Sanitize(r.Owner))
	}
	if err := validateRepositoryName(r.Name); err != nil {
		return NewError(ErrorInvalidReference, "repository", Sanitize(r.Name))
	}
	parsed, err := url.Parse(r.URL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" {
		return NewError(ErrorInvalidResponse, "url", Sanitize(r.URL))
	}
	// parsed.Host carries an explicit port, so a ported canonical URL never
	// matches an identity whose host has none.
	if !strings.EqualFold(parsed.Host, r.Host) {
		return NewError(ErrorInvalidResponse, "url", Sanitize(r.URL))
	}
	if strings.Trim(parsed.Path, "/") != r.Owner+"/"+r.Name {
		return NewError(ErrorInvalidResponse, "url", Sanitize(r.URL))
	}
	return nil
}

// IssueURL builds the canonical browser URL of one issue from the confirmed
// identity. It never takes a URL from remote content.
func (r Repository) IssueURL(number int) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	if number <= 0 {
		return "", NewError(ErrorInvalidQuery, "number", strconv.Itoa(number))
	}
	return "https://" + r.Host + "/" + r.Owner + "/" + r.Name + "/issues/" + strconv.Itoa(number), nil
}
