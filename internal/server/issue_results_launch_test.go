package server

import (
	"errors"
	"os"
	"testing"

	"github.com/dualface/kander/internal/issue"
)

func TestIssueResultLaunchReceiptReplaysCompletedAndRetainsUncertain(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "uncertain"}[fail], func(t *testing.T) {
			f := newIssueResultFixture(t)
			calls := 0
			var evidence string
			issue.SetTriageStarter(func(request issue.TriageLaunch) (issue.TriageOutcome, error) {
				calls++
				if !request.ResultSync || request.Action == nil || request.CardSnapshot == nil || request.CardSnapshot.TaskID != f.cardID || request.ProjectDir == "" {
					t.Fatal("result launch lacks private service facts")
				}
				evidence = request.JSONPath
				if _, err := os.Stat(evidence); err != nil {
					t.Fatal(err)
				}
				if fail {
					return issue.TriageOutcome{}, errors.New("unknown terminal effect")
				}
				return issue.TriageOutcome{Address: "tmux:$1:@1:%1", Agent: "codex", Launcher: "tmux"}, nil
			})
			t.Cleanup(func() { issue.SetTriageStarter(nil) })
			options := issue.TriageOptions{CardID: f.cardID, Agent: "codex", Launcher: "tmux"}
			first, err := issue.StartResult(f.ctxA, f.provider, f.rootA, f.repo, 42, options)
			if fail && err == nil || !fail && err != nil {
				t.Fatalf("first launch: %+v %v", first, err)
			}
			second, err := issue.StartResult(f.ctxA, f.provider, f.rootA, f.repo, 42, options)
			if fail && err == nil || !fail && (err != nil || second.Address != first.Address) {
				t.Fatalf("launch replay: %+v %v", second, err)
			}
			if calls != 1 {
				t.Fatal("unknown or completed launch was repeated")
			}
			if _, err := os.Stat(evidence); err != nil {
				t.Fatal("recovery evidence removed")
			}
			options.OperationID = "intentionally-new-session"
			_, _ = issue.StartResult(f.ctxA, f.provider, f.rootA, f.repo, 42, options)
			if calls != 2 {
				t.Fatal("explicit new operation did not obtain a distinct launch journal")
			}
		})
	}
}
