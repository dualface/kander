package issue

import (
	"errors"
	"strings"
)

// ErrorKind is a stable, machine-readable failure category. Callers map it to
// localized messages; the kind itself never changes with the message catalog.
type ErrorKind string

// Structured failure categories of repository resolution and provider access.
const (
	ErrorInvalidReference ErrorKind = "invalid-reference"
	ErrorInvalidQuery     ErrorKind = "invalid-query"
	ErrorNotRepository    ErrorKind = "not-repository"
	ErrorNoRemote         ErrorKind = "no-remote"
	ErrorAmbiguousRemotes ErrorKind = "ambiguous-remotes"
	ErrorInsecureRemote   ErrorKind = "insecure-remote"
	ErrorInvalidDirectory ErrorKind = "invalid-directory"
	ErrorGitUnavailable   ErrorKind = "git-unavailable"
	ErrorCLIUnavailable   ErrorKind = "cli-unavailable"
	ErrorCLIUnsupported   ErrorKind = "cli-unsupported"
	ErrorUnauthenticated  ErrorKind = "unauthenticated"
	ErrorUnauthorized     ErrorKind = "unauthorized"
	ErrorSSORequired      ErrorKind = "sso-required"
	ErrorNotFound         ErrorKind = "not-found"
	ErrorNotAnIssue       ErrorKind = "not-an-issue"
	ErrorLimitExceeded    ErrorKind = "limit-exceeded"
	ErrorRateLimited      ErrorKind = "rate-limited"
	ErrorTimeout          ErrorKind = "timeout"
	ErrorOutputLimit      ErrorKind = "output-limit"
	ErrorInvalidResponse  ErrorKind = "invalid-response"
	ErrorCommandFailed    ErrorKind = "command-failed"
	ErrorImportConflict   ErrorKind = "import-conflict"
)

// Error is a structured failure from repository resolution. Detail and
// Candidates are sanitized and redacted before they reach this type.
type Error struct {
	Kind       ErrorKind
	Op         string
	Host       string
	Detail     string
	Candidates []string
	cause      error
}

// Error implements the error interface.
func (e *Error) Error() string {
	parts := []string{string(e.Kind)}
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if e.Host != "" {
		parts = append(parts, e.Host)
	}
	if e.Detail != "" {
		parts = append(parts, e.Detail)
	}
	if len(e.Candidates) > 0 {
		parts = append(parts, strings.Join(e.Candidates, ","))
	}
	return strings.Join(parts, ": ")
}

// Unwrap exposes the underlying process or decoding failure, if any.
func (e *Error) Unwrap() error { return e.cause }

// KindOf reports the structured kind of err, or "" when err is not an *Error.
func KindOf(err error) ErrorKind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	return ""
}

// NewError builds a structured error with one sanitized detail line.
func NewError(kind ErrorKind, op, detail string) *Error {
	return &Error{Kind: kind, Op: op, Detail: detail}
}

// WrapError builds a structured error that keeps the underlying cause.
func WrapError(err error, kind ErrorKind, op, detail string) *Error {
	return &Error{Kind: kind, Op: op, Detail: detail, cause: err}
}
