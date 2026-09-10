package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

// RepositoryResolver resolves the canonical repository identity for an
// explicit [HOST/]OWNER/REPO reference or for a working directory. Providers
// such as internal/issue/ghcli implement it; the command layer never imports a
// concrete provider.
type RepositoryResolver interface {
	ResolveRepository(ctx context.Context, directory string, explicit string) (Repository, error)
}

// Command returns the `kander issue` runner bound to a provider factory. The
// factory runs once per invocation so every call re-reads the environment.
func Command(factory func() RepositoryResolver) func(args []string) int {
	return func(args []string) int {
		return runWith(factory, args, os.Stdout, os.Stderr)
	}
}

func runWith(factory func() RepositoryResolver, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, config.Text("issue.usage"))
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, config.Text("issue.usage"))
		return 0
	case "repo":
		return runRepo(factory, args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", args[0]))
		fmt.Fprintln(stderr, config.Text("issue.usage"))
		return 2
	}
}

type repoOptions struct {
	repository string
	hasRepo    bool
	json       bool
}

func runRepo(factory func() RepositoryResolver, args []string, stdout, stderr io.Writer) int {
	var options repoOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, config.Text("issue.usage"))
			return 0
		case arg == "--json":
			options.json = true
		case arg == "--repo":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--repo"))
				return 2
			}
			index++
			options.repository = args[index]
			options.hasRepo = true
		case strings.HasPrefix(arg, "--repo="):
			options.repository = strings.TrimPrefix(arg, "--repo=")
			options.hasRepo = true
		default:
			fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
			fmt.Fprintln(stderr, config.Text("issue.usage"))
			return 2
		}
	}
	if options.hasRepo && strings.TrimSpace(options.repository) == "" {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--repo"))
		return 2
	}
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resolver := factory()
	if resolver == nil {
		fmt.Fprintln(stderr, config.Text("issue.error_cli_unavailable", "no repository provider is registered"))
		return 1
	}
	repository, err := resolver.ResolveRepository(ctx, directory, options.repository)
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	if options.json {
		if err := writeRepositoryJSON(stdout, repository); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
		return 0
	}
	writeRepositoryText(stdout, repository)
	return 0
}

type repositoryJSON struct {
	Host    string `json:"host"`
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Private bool   `json:"private"`
	Remote  string `json:"remote"`
}

func writeRepositoryJSON(w io.Writer, repository Repository) error {
	encoded, err := json.Marshal(repositoryJSON{
		Host:    repository.Host,
		Owner:   repository.Owner,
		Name:    repository.Name,
		URL:     repository.URL,
		Private: repository.Private,
		Remote:  repository.Remote,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

func writeRepositoryText(w io.Writer, repository Repository) {
	fmt.Fprintln(w, config.Text("issue.repo_identity", repository.Host, repository.Owner, repository.Name, repository.URL))
	visibility := config.Text("issue.repo_visibility_public")
	if repository.Private {
		visibility = config.Text("issue.repo_visibility_private")
	}
	fmt.Fprintln(w, config.Text("issue.repo_visibility", visibility))
	if repository.Remote != "" {
		fmt.Fprintln(w, config.Text("issue.repo_remote", repository.Remote))
	}
}

func formatError(err error) string {
	var structured *Error
	if !errors.As(err, &structured) {
		return "kander issue: " + Sanitize(err.Error())
	}
	var message string
	switch structured.Kind {
	case ErrorInvalidReference:
		message = config.Text("issue.error_invalid_repository", structured.Detail)
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
	return "kander issue: " + message
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
	default:
		return ""
	}
}
