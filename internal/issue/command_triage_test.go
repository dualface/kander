package issue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestIssueTriageUsageErrors(t *testing.T) {
	useChinese(t)
	importTestBoard(t)
	stub := &stubResolver{snapshot: importTestSnapshot(importTestRepository(), 42)}
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"help", []string{"triage", "--help"}, 0, config.Text("issue.triage_usage")},
		{"missing number", []string{"triage"}, 2, config.Text("issue.error_missing_value", "NUMBER")},
		{"invalid number", []string{"triage", "abc"}, 2, config.Text("issue.error_invalid_value", "NUMBER", "abc")},
		{"zero number", []string{"triage", "0"}, 2, config.Text("issue.error_invalid_value", "NUMBER", "0")},
		{"unknown option", []string{"triage", "42", "--nope"}, 2, config.Text("issue.error_unknown_argument", "--nope")},
		{"missing value", []string{"triage", "42", "--card"}, 2, config.Text("issue.error_missing_value", "--card")},
		{"extra positional", []string{"triage", "42", "43"}, 2, config.Text("issue.error_unknown_argument", "43")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runIssue(t, stub, tc.args...)
			if code != tc.code {
				t.Fatalf("code=%d want=%d stderr=%q", code, tc.code, stderr)
			}
			if !strings.Contains(stderr, tc.want) && !strings.Contains(stdout, tc.want) {
				t.Fatalf("output missing %q\nstdout=%s\nstderr=%s", tc.want, stdout, stderr)
			}
			if tc.code != 0 && stdout != "" {
				t.Fatalf("usage error wrote stdout: %q", stdout)
			}
		})
	}
}

func TestIssueTriageStartsThroughTheSharedPath(t *testing.T) {
	useChinese(t)
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{repository: repository, snapshot: importTestSnapshot(repository, 42)}
	var captured TriageLaunch
	SetTriageStarter(func(request TriageLaunch) (TriageOutcome, error) {
		captured = request
		return TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "session:win:pane"}, nil
	})
	t.Cleanup(func() { SetTriageStarter(nil) })

	code, stdout, stderr := runIssue(t, stub, "triage", "42", "--agent", "claude", "--launcher=tmux")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, config.Text("issue.triage_started", "claude", "tmux", "dualface/kander#42", "session:win:pane")) {
		t.Fatalf("stdout=%s", stdout)
	}
	if captured.Number != 42 || captured.CardID != "" || captured.Agent != "claude" || captured.Launcher != "tmux" {
		t.Fatalf("captured=%+v", captured)
	}
	if captured.JSONPath != filepath.Join(root, ".kander", "caches", "triage", "dualface-kander-42", TriageJSONName) {
		t.Fatalf("json path=%s", captured.JSONPath)
	}
	if _, err := os.Stat(captured.MarkdownPath); err != nil {
		t.Fatalf("evidence missing: %v", err)
	}

	imported, err := Import(context.Background(), stub, root, repository, 42, ImportOptions{Language: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runIssue(t, stub, "triage", "42", "--card", imported.TaskID)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if captured.CardID != imported.TaskID {
		t.Fatalf("captured=%+v", captured)
	}
	if !strings.Contains(stdout, config.Text("issue.triage_started", "claude", "tmux", "dualface/kander#42", "session:win:pane")) {
		t.Fatalf("stdout=%s", stdout)
	}
}

func TestIssueTriageReportsFailures(t *testing.T) {
	useChinese(t)
	importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{repository: repository, snapshot: importTestSnapshot(repository, 42)}

	SetTriageStarter(nil)
	code, stdout, stderr := runIssue(t, stub, "triage", "42")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, config.Text("issue.error_cli_unavailable", "no takeover launcher is registered")) {
		t.Fatalf("stderr=%s", stderr)
	}

	SetTriageStarter(func(TriageLaunch) (TriageOutcome, error) {
		return TriageOutcome{}, errors.New("launch fake failure")
	})
	t.Cleanup(func() { SetTriageStarter(nil) })
	code, stdout, stderr = runIssue(t, stub, "triage", "42")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, "kander issue: launch fake failure") {
		t.Fatalf("stderr=%s", stderr)
	}
	if strings.Contains(stderr, "GitHub CLI") {
		t.Fatalf("launch error was reworded: %s", stderr)
	}

	stub.issueErr = NewError(ErrorNotFound, "get", "42")
	code, stdout, stderr = runIssue(t, stub, "triage", "42")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, config.Text("issue.error_not_found", "42")) {
		t.Fatalf("stderr=%s", stderr)
	}

	stub.issueErr = nil
	code, stdout, stderr = runIssue(t, stub, "triage", "42", "--card", "20260101-not-bound-task")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, "20260101-not-bound-task") {
		t.Fatalf("stderr=%s", stderr)
	}
}
