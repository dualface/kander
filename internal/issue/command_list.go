package issue

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

type listOptions struct {
	repository string
	hasRepo    bool
	state      string
	labels     []string
	search     string
	limit      int
	json       bool
}

// runList implements `kander issue list`.
func runList(factory func() IssueProvider, args []string, stdout, stderr io.Writer) int {
	var options listOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, config.Text("issue.list_usage"))
			return 0
		case arg == "--json":
			options.json = true
		case arg == "--state":
			value, next, ok := optionValue(args, index, "--state", stderr)
			if !ok {
				return 2
			}
			index = next
			options.state = value
		case arg == "--label":
			value, next, ok := optionValue(args, index, "--label", stderr)
			if !ok {
				return 2
			}
			index = next
			options.labels = append(options.labels, value)
		case arg == "--search":
			value, next, ok := optionValue(args, index, "--search", stderr)
			if !ok {
				return 2
			}
			index = next
			options.search = value
		case arg == "--limit":
			value, next, ok := optionValue(args, index, "--limit", stderr)
			if !ok {
				return 2
			}
			index = next
			limit, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				fmt.Fprintln(stderr, config.Text("issue.error_invalid_value", "--limit", value))
				return 2
			}
			options.limit = limit
		case arg == "--repo":
			value, next, ok := optionValue(args, index, "--repo", stderr)
			if !ok {
				return 2
			}
			index = next
			options.repository = value
			options.hasRepo = true
		default:
			fmt.Fprintln(stderr, config.Text("issue.error_unknown_argument", arg))
			fmt.Fprintln(stderr, config.Text("issue.list_usage"))
			return 2
		}
	}
	if options.hasRepo && strings.TrimSpace(options.repository) == "" {
		fmt.Fprintln(stderr, config.Text("issue.error_missing_value", "--repo"))
		return 2
	}
	query, err := (IssueQuery{
		State:  options.state,
		Labels: options.labels,
		Search: options.search,
		Limit:  options.limit,
	}).Normalize()
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 2
	}
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	ctx, cancel := commandContext()
	defer cancel()
	page, err := provider.ListIssues(ctx, repository, query)
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	if options.json {
		if err := writeIssuePageJSON(stdout, page); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
		return 0
	}
	writeIssuePageText(stdout, page)
	return 0
}

func writeIssuePageText(w io.Writer, page IssuePage) {
	for _, summary := range page.Issues {
		fmt.Fprintln(w, config.Text("issue.list_item", itoa(summary.Number), summary.State, summary.Title))
		meta := []string{config.Text("issue.list_updated", formatTime(summary.UpdatedAt))}
		if len(summary.Labels) > 0 {
			meta = append(meta, config.Text("issue.list_labels", strings.Join(summary.Labels, ", ")))
		}
		fmt.Fprintln(w, config.Text("issue.list_item_meta", strings.Join(meta, "  ")))
	}
	if page.More {
		fmt.Fprintln(w, config.Text("issue.list_summary_more", itoa(len(page.Issues)), itoa(page.Limit)))
		return
	}
	fmt.Fprintln(w, config.Text("issue.list_summary", itoa(len(page.Issues))))
}

type issueSummaryJSON struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	URL       string   `json:"url"`
	Labels    []string `json:"labels"`
	UpdatedAt string   `json:"updated_at"`
}

type issuePageJSON struct {
	Repository repositoryJSON     `json:"repository"`
	Issues     []issueSummaryJSON `json:"issues"`
	Limit      int                `json:"limit"`
	More       bool               `json:"more"`
}

func writeIssuePageJSON(w io.Writer, page IssuePage) error {
	encoded, err := json.Marshal(issuePageJSON{
		Repository: repositoryView(page.Repository),
		Issues:     summaryViews(page.Issues),
		Limit:      page.Limit,
		More:       page.More,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

func summaryViews(issues []IssueSummary) []issueSummaryJSON {
	out := make([]issueSummaryJSON, 0, len(issues))
	for _, summary := range issues {
		labels := summary.Labels
		if labels == nil {
			labels = []string{}
		}
		out = append(out, issueSummaryJSON{
			Number:    summary.Number,
			Title:     summary.Title,
			State:     summary.State,
			URL:       summary.URL,
			Labels:    labels,
			UpdatedAt: summary.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
