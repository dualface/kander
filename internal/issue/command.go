package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// RepositoryResolver resolves the canonical repository identity for an
// explicit [HOST/]OWNER/REPO reference or for a working directory. Providers
// such as internal/issue/ghcli implement it; the command layer never imports a
// concrete provider.
type RepositoryResolver interface {
	ResolveRepository(ctx context.Context, directory string, explicit string) (Repository, error)
}

// NewCommand returns `kander issue` bound to a provider factory. The factory runs once per
// invocation so every call re-reads the environment.
func NewCommand(factory func() IssueProvider) *cobra.Command {
	return newCommand(factory, os.Stdout, os.Stderr)
}

func newCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	return command.Group("issue",
		newRepoCommand(factory, stdout, stderr),
		newListCommand(factory, stdout, stderr),
		newShowCommand(factory, stdout, stderr),
		command.RequireProject(newImportCommand(factory, stdout, stderr)),
		command.RequireProject(newResultCommand(factory, stdout, stderr)),
		command.RequireProject(newTriageCommand(factory, stdout, stderr)),
	)
}

// nonBlank is the validator of a flag whose value must not be empty.
func nonBlank(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return command.Usage("issue.error_missing_value", "--"+name)
		}
		return nil
	}
}

// addRepoFlag registers --repo, which names the repository explicitly.
func addRepoFlag(c *cobra.Command, repository *string) {
	command.Checked(c.Flags(), repository, "repo", "", nonBlank("repo"))
}

// numberArgs accepts exactly one positive issue number.
func numberArgs(c *cobra.Command, args []string) error {
	if err := command.Args(1, 1)(c, args); err != nil {
		return err
	}
	if number, err := strconv.Atoi(strings.TrimSpace(args[0])); err != nil || number <= 0 {
		return command.Usage("issue.error_invalid_value", "NUMBER", args[0])
	}
	return nil
}

// issueNumber is the number numberArgs accepted.
func issueNumber(args []string) int {
	number, _ := strconv.Atoi(strings.TrimSpace(args[0]))
	return number
}

type repoOptions struct {
	repository string
	hasRepo    bool
	json       bool
}

func newRepoCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	var options repoOptions
	c := &cobra.Command{Use: "repo", Args: command.Args(0, 0)}
	c.RunE = func(*cobra.Command, []string) error {
		options.hasRepo = c.Flags().Changed("repo")
		return command.Exit(runRepo(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	c.Flags().BoolVar(&options.json, "json", false, "")
	return c
}

func runRepo(factory func() IssueProvider, options repoOptions, stdout, stderr io.Writer) int {
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

// commandContext bounds one provider interaction the same way for every
// subcommand.
func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
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
