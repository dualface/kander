package issue

import (
	"context"
	"encoding/json"
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
func Command(factory func() IssueProvider) func(args []string) int {
	return func(args []string) int {
		return runWith(factory, args, os.Stdout, os.Stderr)
	}
}

func runWith(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText())
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usageText())
		return 0
	case "repo":
		return runRepo(factory, args[1:], stdout, stderr)
	case "list":
		return runList(factory, args[1:], stdout, stderr)
	case "show":
		return runShow(factory, args[1:], stdout, stderr)
	case "import":
		return runImport(factory, args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", args[0]))
		fmt.Fprintln(stderr, usageText())
		return 2
	}
}

func usageText() string {
	return config.Text("issue.usage")
}

// commandContext bounds one provider interaction the same way for every
// subcommand.
func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

type repoOptions struct {
	repository string
	hasRepo    bool
	json       bool
}

func runRepo(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	var options repoOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, usageText())
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
			fmt.Fprintln(stderr, usageText())
			return 2
		}
	}
	if options.hasRepo && strings.TrimSpace(options.repository) == "" {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--repo"))
		return 2
	}
	repository, code := resolveRepository(factory(), options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
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

// resolveRepository resolves the working directory once and maps every failure
// to the shared command error format. The caller passes the provider it built
// so one invocation never constructs two providers.
func resolveRepository(provider IssueProvider, explicit string, hasRepo bool, stderr io.Writer) (Repository, int) {
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return Repository{}, 1
	}
	ctx, cancel := commandContext()
	defer cancel()
	if provider == nil {
		fmt.Fprintln(stderr, config.Text("issue.error_cli_unavailable", "no repository provider is registered"))
		return Repository{}, 1
	}
	value := explicit
	if !hasRepo {
		value = ""
	}
	repository, err := provider.ResolveRepository(ctx, directory, value)
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return Repository{}, 1
	}
	return repository, 0
}

type repositoryJSON struct {
	Host    string `json:"host"`
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Private bool   `json:"private"`
	Remote  string `json:"remote"`
}

func repositoryView(repository Repository) repositoryJSON {
	return repositoryJSON{
		Host:    repository.Host,
		Owner:   repository.Owner,
		Name:    repository.Name,
		URL:     repository.URL,
		Private: repository.Private,
		Remote:  repository.Remote,
	}
}

func writeRepositoryJSON(w io.Writer, repository Repository) error {
	encoded, err := json.Marshal(repositoryView(repository))
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
	return "kander issue: " + Message(err)
}

// splitLongOption recognizes --name=value and returns the inline value.
func splitLongOption(arg, name string) (string, bool) {
	prefix := name + "="
	if !strings.HasPrefix(arg, prefix) {
		return "", false
	}
	return strings.TrimPrefix(arg, prefix), true
}

// optionValue consumes the value of a long option that takes one argument.
// It returns the value, the next index and whether parsing succeeded.
func optionValue(args []string, index int, name string, stderr io.Writer) (string, int, bool) {
	arg := args[index]
	if inline, ok := splitLongOption(arg, name); ok {
		return inline, index, true
	}
	if index+1 >= len(args) {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", name))
		return "", index, false
	}
	return args[index+1], index + 1, true
}
