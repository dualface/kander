package issue

import (
	"errors"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// Message renders one issue error as a localized, actionable sentence without
// the command prefix. The command layer adds its own prefix; the TUI shows the
// message directly.
func Message(err error) string {
	var structured *Error
	if !errors.As(err, &structured) {
		return Sanitize(err.Error())
	}
	var message string
	switch structured.Kind {
	case ErrorInvalidReference:
		message = config.Text("issue.error_invalid_repository", structured.Detail)
	case ErrorInvalidQuery:
		message = config.Text("issue.error_invalid_query", structured.Op, structured.Detail)
	case ErrorNotRepository:
		message = config.Text("issue.error_not_repository", structured.Detail)
	case ErrorNoRemote:
		message = config.Text("issue.error_no_remote")
	case ErrorAmbiguousRemotes:
		message = config.Text("issue.error_ambiguous_remotes", strings.Join(structured.Candidates, ", "))
	case ErrorInsecureRemote:
		message = config.Text("issue.error_insecure_remote", structured.Detail)
	case ErrorGitUnavailable:
		message = config.Text("issue.error_git_unavailable", structured.Detail)
	case ErrorCLIUnavailable:
		message = config.Text("issue.error_cli_unavailable", structured.Detail)
	case ErrorCLIUnsupported:
		message = config.Text("issue.error_cli_unsupported", structured.Detail)
	case ErrorInvalidDirectory:
		message = config.Text("issue.error_invalid_directory", structured.Detail)
	case ErrorUnauthenticated:
		message = config.Text("issue.error_unauthenticated", structured.Host)
	case ErrorUnauthorized:
		message = config.Text("issue.error_unauthorized", structured.Host)
	case ErrorSSORequired:
		message = config.Text("issue.error_sso_required", structured.Host)
	case ErrorNotFound:
		message = config.Text("issue.error_not_found", structured.Detail)
	case ErrorNotAnIssue:
		message = config.Text("issue.error_not_an_issue", structured.Detail)
	case ErrorLimitExceeded:
		message = config.Text("issue.error_limit_exceeded", structured.Op, structured.Detail)
	case ErrorRateLimited:
		message = config.Text("issue.error_rate_limited", structured.Host)
	case ErrorTimeout:
		message = config.Text("issue.error_timeout")
	case ErrorOutputLimit:
		message = config.Text("issue.error_output_limit")
	case ErrorInvalidResponse:
		message = config.Text("issue.error_invalid_response", structured.Detail)
	default:
		message = config.Text("issue.error_command_failed", structured.Detail)
	}
	if hint := errorHint(structured); hint != "" {
		message += " " + hint
	}
	return message
}

func errorHint(structured *Error) string {
	switch structured.Kind {
	case ErrorNotRepository:
		return config.Text("issue.remediation_repo_flag")
	case ErrorNoRemote, ErrorAmbiguousRemotes:
		return config.Text("issue.remediation_repo_flag") + " " + config.Text("issue.remediation_set_default")
	case ErrorUnauthenticated:
		if structured.Host != "" {
			return config.Text("issue.remediation_auth_login_host", structured.Host)
		}
		return config.Text("issue.remediation_auth_login")
	case ErrorNotAnIssue:
		return config.Text("issue.remediation_issue_number")
	default:
		return ""
	}
}
