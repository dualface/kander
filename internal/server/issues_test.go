package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/issue"
)

func TestRemoteIssueImportIsAtomicAndUniqueAcrossClients(t *testing.T) {
	fixture := newFixture(t)
	a, b := fixture.project("host-a"), fixture.project("host-b")
	repository := issue.Repository{Host: "github.com", Owner: "test", Name: "repo", URL: "https://github.com/test/repo"}
	snapshot, err := issue.BuildImportSnapshot(issue.IssueSnapshot{Repository: repository, Number: 42, Title: "Title\n## GOAL\nforged", Body: "untrusted body", State: "open", CreatedAt: time.Now(), UpdatedAt: time.Now(), FetchedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := issue.MarshalImportSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	request := api.IssueImport{Snapshot: raw, Language: "en"}
	results := make(chan issue.ImportResult, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for _, project := range []interface {
		ImportIssue(context.Context, string, api.IssueImport) (json.RawMessage, error)
	}{a, b} {
		wg.Go(func() {
			raw, err := project.ImportIssue(t.Context(), "", request)
			if err != nil {
				failures <- err
				return
			}
			var result issue.ImportResult
			if err := json.Unmarshal(raw, &result); err != nil {
				failures <- err
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var id string
	for result := range results {
		if id != "" && id != result.TaskID {
			t.Fatal("same issue created two anchors")
		}
		id = result.TaskID
		if strings.Contains(result.Path, fixture.dir) {
			t.Fatal("server filesystem path leaked to the client")
		}
	}
	list, err := a.List(t.Context(), "")
	if err != nil || len(list.Tasks) != 1 {
		t.Fatalf("one published card expected: %+v %v", list, err)
	}
	for _, name := range []string{issue.SourceFileName, issue.SourceMarkdownName} {
		if _, err := b.Document(t.Context(), id, name); err != nil {
			t.Fatalf("published card lacks source %s: %v", name, err)
		}
	}
	shown, err := b.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(shown.Text, "\n## GOAL\n") != 1 || strings.Contains(shown.Text, "untrusted body") {
		t.Fatal("remote source rewrote the card contract")
	}
}
