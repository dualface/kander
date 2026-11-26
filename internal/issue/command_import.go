package issue

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
)

type issueImportOptions struct {
	repository string
	hasRepo    bool
	number     int
	comments   bool
	taskType   string
	large      bool
	language   string
}

func newImportCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	var options issueImportOptions
	c := &cobra.Command{Use: "import NUMBER", Args: numberArgs}
	c.RunE = func(_ *cobra.Command, args []string) error {
		options.hasRepo, options.number = c.Flags().Changed("repo"), issueNumber(args)
		return command.Exit(runImport(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	c.Flags().BoolVar(&options.comments, "comments", false, "")
	command.Checked(c.Flags(), &options.taskType, "type", "", func(value string) error {
		if !importTaskTypeAllowed(strings.TrimSpace(value)) {
			return command.Usage("issue.error_invalid_value", "--type", value)
		}
		return nil
	})
	c.Flags().BoolVar(&options.large, "large", false, "")
	command.Checked(c.Flags(), &options.language, "language", "", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return command.Usage("issue.error_missing_value", "--language")
		}
		if _, err := config.ValidateAgentLanguage(strings.TrimSpace(value)); err != nil {
			return command.Usage("config.agent_language_invalid")
		}
		return nil
	})
	return c
}

// runImport implements `kander issue import NUMBER`. The command resolves the
// repository before it touches the network, so a usage or location error never
// costs an API call.
func runImport(factory func() IssueProvider, options issueImportOptions, stdout, stderr io.Writer) int {
	taskType := strings.TrimSpace(options.taskType)
	language := strings.TrimSpace(options.language)
	if language != "" {
		language, _ = config.ValidateAgentLanguage(language)
	} else {
		cfg, err := config.Load(false)
		if err != nil {
			return fail(stderr, err, 1)
		}
		language = cfg.AgentLanguage
	}

	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	ctx, cancel := commandContext()
	defer cancel()
	importOptions := ImportOptions{Type: taskType, Large: options.large, Language: language, Comments: options.comments}
	result, err := importCard(ctx, provider, repository, options.number, importOptions)
	if err != nil {
		return fail(stderr, err, 1)
	}
	if command.JSON() {
		return command.Result(importView(result))
	}
	writeImportText(stdout, result)
	return 0
}

func importTaskTypeAllowed(value string) bool {
	for _, allowed := range board.TaskTypes() {
		if value == allowed {
			return true
		}
	}
	return false
}

func writeImportText(w io.Writer, result ImportResult) {
	key := "issue.import_created"
	if result.Existing {
		key = "issue.import_existing"
	}
	fmt.Fprintln(w, config.Text(key, result.TaskID, result.State))
	fmt.Fprintln(w, config.Text("issue.import_source", result.SourceKey))
	fmt.Fprintln(w, config.Text("issue.import_path", result.Path))
	if result.CommentsLoaded {
		fmt.Fprintln(w, config.Text("issue.import_comments_loaded"))
	} else {
		fmt.Fprintln(w, config.Text("issue.import_comments_skipped"))
	}
}

type issueImportJSON struct {
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	Path           string `json:"path"`
	Existing       bool   `json:"existing"`
	SourceKey      string `json:"source_key"`
	SourceURL      string `json:"source_url"`
	CommentsLoaded bool   `json:"comments_loaded"`
}

// importView is the result object of kander issue import.
func importView(result ImportResult) issueImportJSON {
	return issueImportJSON{
		TaskID:         result.TaskID,
		State:          result.State,
		Path:           result.Path,
		Existing:       result.Existing,
		SourceKey:      result.SourceKey,
		SourceURL:      result.SourceURL,
		CommentsLoaded: result.CommentsLoaded,
	}
}
