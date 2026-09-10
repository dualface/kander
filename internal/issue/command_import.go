package issue

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

type issueImportOptions struct {
	repository string
	hasRepo    bool
	number     int
	comments   bool
	taskType   string
	large      bool
	language   string
	json       bool
}

// runImport implements `kander issue import NUMBER`. The command resolves the
// board root and the repository before it touches the network, so a usage or
// location error never costs an API call.
func runImport(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	var options issueImportOptions
	positional := 0
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, config.Text("issue.import_usage"))
			return 0
		case arg == "--json":
			options.json = true
		case arg == "--comments":
			options.comments = true
		case arg == "--large":
			options.large = true
		case arg == "--repo":
			value, next, ok := optionValue(args, index, "--repo", stderr)
			if !ok {
				return 2
			}
			index = next
			options.repository = value
			options.hasRepo = true
		case arg == "--type":
			value, next, ok := optionValue(args, index, "--type", stderr)
			if !ok {
				return 2
			}
			index = next
			options.taskType = value
		case arg == "--language":
			value, next, ok := optionValue(args, index, "--language", stderr)
			if !ok {
				return 2
			}
			index = next
			options.language = value
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
			fmt.Fprintln(stderr, config.Text("issue.import_usage"))
			return 2
		default:
			positional++
			if positional > 1 {
				fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
				fmt.Fprintln(stderr, config.Text("issue.import_usage"))
				return 2
			}
			number, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil || number <= 0 {
				fmt.Fprintln(stderr, config.Text("issue.error_invalid_value", "NUMBER", arg))
				return 2
			}
			options.number = number
		}
	}
	if positional == 0 {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "NUMBER"))
		fmt.Fprintln(stderr, config.Text("issue.import_usage"))
		return 2
	}
	if options.hasRepo && strings.TrimSpace(options.repository) == "" {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--repo"))
		return 2
	}
	taskType := strings.TrimSpace(options.taskType)
	if taskType != "" && !importTaskTypeAllowed(taskType) {
		fmt.Fprintln(stderr, config.Text("issue.error_invalid_value", "--type", options.taskType))
		return 2
	}
	language := strings.TrimSpace(options.language)
	if options.language != "" && language == "" {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--language"))
		return 2
	}
	if language != "" {
		validated, err := config.ValidateAgentLanguage(language)
		if err != nil {
			fmt.Fprintln(stderr, config.Text("config.agent_language_invalid"))
			return 2
		}
		language = validated
	} else {
		cfg, err := config.Load(false)
		if err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
		language = cfg.AgentLanguage
	}
	root, err := board.BoardRoot()
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	ctx, cancel := commandContext()
	defer cancel()
	result, err := Import(ctx, provider, root, repository, options.number, ImportOptions{
		Type:     taskType,
		Large:    options.large,
		Language: language,
		Comments: options.comments,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	if options.json {
		if err := writeImportJSON(stdout, result); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
		return 0
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

func writeImportJSON(w io.Writer, result ImportResult) error {
	encoded, err := json.Marshal(issueImportJSON{
		TaskID:         result.TaskID,
		State:          result.State,
		Path:           result.Path,
		Existing:       result.Existing,
		SourceKey:      result.SourceKey,
		SourceURL:      result.SourceURL,
		CommentsLoaded: result.CommentsLoaded,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}
