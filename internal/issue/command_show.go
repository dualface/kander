package issue

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

type showOptions struct {
	repository string
	hasRepo    bool
	number     int
	comments   bool
	json       bool
}

func newShowCommand(factory func() IssueProvider, stdout, stderr io.Writer) *cobra.Command {
	var options showOptions
	c := &cobra.Command{Use: "show NUMBER", Args: numberArgs}
	c.RunE = func(_ *cobra.Command, args []string) error {
		options.hasRepo, options.number = c.Flags().Changed("repo"), issueNumber(args)
		return command.Exit(runShow(factory, options, stdout, stderr))
	}
	addRepoFlag(c, &options.repository)
	c.Flags().BoolVar(&options.comments, "comments", false, "")
	c.Flags().BoolVar(&options.json, "json", false, "")
	return c
}

// runShow implements `kander issue show NUMBER`.
func runShow(factory func() IssueProvider, options showOptions, stdout, stderr io.Writer) int {
	provider := factory()
	repository, code := resolveRepository(provider, options.repository, options.hasRepo, stderr)
	if code != 0 {
		return code
	}
	ctx, cancel := commandContext()
	defer cancel()
	snapshot, err := provider.GetIssue(ctx, repository, options.number, options.comments)
	if err != nil {
		fmt.Fprintln(stderr, formatError(err))
		return 1
	}
	if options.json {
		if err := writeIssueSnapshotJSON(stdout, snapshot); err != nil {
			fmt.Fprintln(stderr, formatError(err))
			return 1
		}
		return 0
	}
	writeIssueSnapshotText(stdout, snapshot)
	return 0
}

func writeIssueSnapshotText(w io.Writer, snapshot IssueSnapshot) {
	fmt.Fprintln(w, config.Text("issue.show_identity", strconv.Itoa(snapshot.Number), snapshot.State, snapshot.Title))
	meta := config.Text("issue.show_meta", orDash(snapshot.Author), formatTime(snapshot.UpdatedAt))
	fmt.Fprintln(w, meta)
	if len(snapshot.Labels) > 0 {
		fmt.Fprintln(w, config.Text("issue.show_labels", strings.Join(snapshot.Labels, ", ")))
	}
	if len(snapshot.Assignees) > 0 {
		fmt.Fprintln(w, config.Text("issue.show_assignees", strings.Join(snapshot.Assignees, ", ")))
	}
	if snapshot.URL != "" {
		fmt.Fprintln(w, config.Text("issue.show_url", snapshot.URL))
	}
	fmt.Fprintln(w)
	if strings.TrimSpace(snapshot.Body) == "" {
		fmt.Fprintln(w, config.Text("issue.show_no_body"))
	} else {
		fmt.Fprint(w, snapshot.Body)
		if !strings.HasSuffix(snapshot.Body, "\n") {
			fmt.Fprintln(w)
		}
	}
	if !snapshot.CommentsLoaded {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, config.Text("issue.show_comments", strconv.Itoa(len(snapshot.Comments))))
	for _, comment := range snapshot.Comments {
		fmt.Fprintln(w, config.Text("issue.show_comment_header", orDash(comment.Author), formatTime(comment.CreatedAt)))
		fmt.Fprint(w, comment.Body)
		if !strings.HasSuffix(comment.Body, "\n") {
			fmt.Fprintln(w)
		}
	}
}

type issueCommentJSON struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
}

type issueShowJSON struct {
	Repository repositoryJSON    `json:"repository"`
	Issue      issueSnapshotJSON `json:"issue"`
}

type issueSnapshotJSON struct {
	Number         int                `json:"number"`
	Title          string             `json:"title"`
	State          string             `json:"state"`
	Body           string             `json:"body"`
	Author         string             `json:"author"`
	URL            string             `json:"url"`
	Labels         []string           `json:"labels"`
	Assignees      []string           `json:"assignees"`
	CreatedAt      string             `json:"created_at"`
	UpdatedAt      string             `json:"updated_at"`
	FetchedAt      string             `json:"fetched_at"`
	CommentsLoaded bool               `json:"comments_loaded"`
	Comments       []issueCommentJSON `json:"comments,omitempty"`
}

func writeIssueSnapshotJSON(w io.Writer, snapshot IssueSnapshot) error {
	labels := snapshot.Labels
	if labels == nil {
		labels = []string{}
	}
	assignees := snapshot.Assignees
	if assignees == nil {
		assignees = []string{}
	}
	view := issueSnapshotJSON{
		Number:         snapshot.Number,
		Title:          snapshot.Title,
		State:          snapshot.State,
		Body:           snapshot.Body,
		Author:         snapshot.Author,
		URL:            snapshot.URL,
		Labels:         labels,
		Assignees:      assignees,
		CreatedAt:      snapshot.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      snapshot.UpdatedAt.UTC().Format(time.RFC3339),
		FetchedAt:      snapshot.FetchedAt.UTC().Format(time.RFC3339),
		CommentsLoaded: snapshot.CommentsLoaded,
	}
	if snapshot.CommentsLoaded {
		comments := make([]issueCommentJSON, 0, len(snapshot.Comments))
		for _, comment := range snapshot.Comments {
			comments = append(comments, issueCommentJSON{
				Author:    comment.Author,
				Body:      comment.Body,
				CreatedAt: comment.CreatedAt.UTC().Format(time.RFC3339),
				URL:       comment.URL,
			})
		}
		view.Comments = comments
	}
	encoded, err := json.Marshal(issueShowJSON{
		Repository: repositoryView(snapshot.Repository),
		Issue:      view,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
