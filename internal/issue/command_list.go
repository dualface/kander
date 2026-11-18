package issue

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

type listOptions struct {
	repository string
	hasRepo    bool
	state      string
	labels     []string
	search     string
	limit      int
}

func newListCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	var options listOptions
	c := &cobra.Command{Use: "list", Args: command.Args(0, 0)}
	c.RunE = func(*cobra.Command, []string) error {
		options.hasRepo = c.Flags().Changed("repo")
		return command.Exit(runList(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	command.Checked(c.Flags(), &options.state, "state", "", nonBlank("state"))
	c.Flags().StringArrayVar(&options.labels, "label", nil, "")
	command.Checked(c.Flags(), &options.search, "search", "", nonBlank("search"))
	c.Flags().IntVar(&options.limit, "limit", 0, "")
	return c
}

// runList implements `kander issue list`.
func runList(factory func() IssueProvider, options listOptions, stdout, stderr io.Writer) int {
	query, err := (IssueQuery{
		State:  options.state,
		Labels: options.labels,
		Search: options.search,
		Limit:  options.limit,
	}).Normalize()
	if err != nil {
		return fail(stderr, err, 2)
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
		return fail(stderr, err, 1)
	}
	if command.JSON() {
		return command.Result(issuePageJSON{
			Repository: repositoryView(page.Repository),
			Issues:     summaryViews(page.Issues),
			Limit:      page.Limit,
			More:       page.More,
		})
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
