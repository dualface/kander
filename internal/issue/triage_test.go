package issue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func triageEvidenceDir(root string) string {
	return filepath.Join(root, ".kander", "caches", "triage", "dualface-kander-42")
}

func TestPrepareTriageWritesRefreshedEvidence(t *testing.T) {
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{snapshot: importTestSnapshot(repository, 42)}
	stub.snapshot.CommentsLoaded = true
	stub.snapshot.Comments = []IssueComment{{Author: "carol", Body: "Confirmed on Linux.", CreatedAt: stub.snapshot.UpdatedAt}}

	evidence, err := PrepareTriage(context.Background(), stub, root, repository, 42)
	if err != nil {
		t.Fatal(err)
	}
	if stub.issueCalls != 1 || stub.issueNumber != 42 || !stub.withComments {
		t.Fatalf("provider calls=%d number=%d comments=%v", stub.issueCalls, stub.issueNumber, stub.withComments)
	}
	if evidence.JSONPath != filepath.Join(triageEvidenceDir(root), TriageJSONName) ||
		evidence.MarkdownPath != filepath.Join(triageEvidenceDir(root), TriageMarkdownName) {
		t.Fatalf("paths=%+v", evidence)
	}
	data, err := os.ReadFile(evidence.JSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var record ImportSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != ImportSchema || record.SourceKey != "github://github.com/dualface/kander/issues/42" {
		t.Fatalf("record=%+v", record)
	}
	if record.Issue.Number != 42 || !record.CommentsLoaded || len(record.Comments) != 1 {
		t.Fatalf("record=%+v", record)
	}
	markdown, err := os.ReadFile(evidence.MarkdownPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Crash when importing an issue", "Steps to reproduce", "@carol", "untrusted remote data"} {
		if !strings.Contains(string(markdown), want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(evidence.JSONPath); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("evidence mode=%v err=%v", info, err)
		}
	}

	// A second attempt refetches and overwrites the evidence instead of reusing
	// the previous copy.
	stub.snapshot.Body = "Second fetch body."
	stub.snapshot.FetchedAt = stub.snapshot.FetchedAt.Add(1)
	stub.snapshot.Comments = nil
	if _, err := PrepareTriage(context.Background(), stub, root, repository, 42); err != nil {
		t.Fatal(err)
	}
	markdown, err = os.ReadFile(evidence.MarkdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "Second fetch body.") {
		t.Fatalf("evidence was not refreshed:\n%s", markdown)
	}
}

func TestPrepareTriageRejectsInvalidTargets(t *testing.T) {
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{snapshot: importTestSnapshot(repository, 42)}

	if _, err := PrepareTriage(context.Background(), nil, root, repository, 42); KindOf(err) != ErrorCLIUnavailable {
		t.Fatalf("nil provider kind=%v err=%v", KindOf(err), err)
	}
	if _, err := PrepareTriage(context.Background(), stub, root, repository, 0); KindOf(err) != ErrorInvalidQuery {
		t.Fatalf("zero number kind=%v err=%v", KindOf(err), err)
	}
	if _, err := PrepareTriage(context.Background(), stub, root, Repository{Host: "github.com"}, 42); KindOf(err) != ErrorInvalidReference {
		t.Fatalf("bad repository kind=%v err=%v", KindOf(err), err)
	}
	if _, err := os.Stat(triageEvidenceDir(root)); !os.IsNotExist(err) {
		t.Fatalf("rejected target wrote evidence: %v", err)
	}
}

func TestPrepareTriageRejectsMismatchedAndOversizeReplies(t *testing.T) {
	root := importTestBoard(t)
	repository := importTestRepository()

	other := importTestRepository()
	other.Name = "tool"
	stub := &stubResolver{snapshot: importTestSnapshot(other, 42)}
	if _, err := PrepareTriage(context.Background(), stub, root, repository, 42); KindOf(err) != ErrorInvalidResponse {
		t.Fatalf("mismatched kind=%v err=%v", KindOf(err), err)
	}

	large := importTestSnapshot(repository, 42)
	large.Body = strings.Repeat("x", MaxIssueSnapshotBytes+1)
	stub = &stubResolver{snapshot: large}
	if _, err := PrepareTriage(context.Background(), stub, root, repository, 42); KindOf(err) != ErrorLimitExceeded {
		t.Fatalf("oversize kind=%v err=%v", KindOf(err), err)
	}

	stub = &stubResolver{issueErr: NewError(ErrorNotFound, "get", "42")}
	if _, err := PrepareTriage(context.Background(), stub, root, repository, 42); KindOf(err) != ErrorNotFound {
		t.Fatalf("provider error kind=%v err=%v", KindOf(err), err)
	}
	if _, err := os.Stat(triageEvidenceDir(root)); !os.IsNotExist(err) {
		t.Fatalf("rejected reply wrote evidence: %v", err)
	}
}

func TestStartTriageUsesStarterAndValidatesBoundCard(t *testing.T) {
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{snapshot: importTestSnapshot(repository, 42)}
	var captured TriageLaunch
	calls := 0
	SetTriageStarter(func(request TriageLaunch) (TriageOutcome, error) {
		calls++
		captured = request
		return TriageOutcome{Agent: request.Agent, Launcher: request.Launcher, Address: "s:w:p"}, nil
	})
	t.Cleanup(func() { SetTriageStarter(nil) })

	outcome, err := StartTriage(context.Background(), stub, root, repository, 42, TriageOptions{Agent: "claude", Launcher: "tmux"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Agent != "claude" || outcome.Launcher != "tmux" || outcome.Address != "s:w:p" {
		t.Fatalf("outcome=%+v", outcome)
	}
	if calls != 1 || captured.Number != 42 || captured.CardID != "" ||
		captured.Agent != "claude" || captured.Launcher != "tmux" || captured.Root != root {
		t.Fatalf("captured=%+v calls=%d", captured, calls)
	}
	if captured.JSONPath == "" || captured.MarkdownPath == "" {
		t.Fatalf("evidence paths missing: %+v", captured)
	}
	if !strings.HasPrefix(captured.JSONPath, triageEvidenceDir(root)) {
		t.Fatalf("json path=%s", captured.JSONPath)
	}

	imported, err := Import(context.Background(), stub, root, repository, 42, ImportOptions{Language: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartTriage(context.Background(), stub, root, repository, 42, TriageOptions{CardID: imported.TaskID}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || captured.CardID != imported.TaskID {
		t.Fatalf("bound card not passed: %+v calls=%d", captured, calls)
	}

	if _, err := StartTriage(context.Background(), stub, root, repository, 42, TriageOptions{CardID: "20260101-not-bound-task"}); KindOf(err) != ErrorInvalidQuery {
		t.Fatalf("unbound card kind=%v err=%v", KindOf(err), err)
	}
	if calls != 2 {
		t.Fatalf("starter ran for an unbound card: %d", calls)
	}
}

func TestStartTriageReportsMissingStarter(t *testing.T) {
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{snapshot: importTestSnapshot(repository, 42)}
	SetTriageStarter(nil)
	if _, err := StartTriage(context.Background(), stub, root, repository, 42, TriageOptions{}); KindOf(err) != ErrorCLIUnavailable {
		t.Fatalf("kind=%v err=%v", KindOf(err), err)
	}
	if _, err := os.Stat(filepath.Join(triageEvidenceDir(root), TriageJSONName)); err != nil {
		t.Fatalf("evidence must be prepared before the starter check: %v", err)
	}
}

func TestTriageErrorKeepsLaunchMessages(t *testing.T) {
	useChinese(t)
	if got := TriageError(nil); got != "" {
		t.Fatalf("nil=%q", got)
	}
	if got := TriageError(errors.New("launch says no")); got != "launch says no" {
		t.Fatalf("plain=%q", got)
	}
	structured := NewError(ErrorInvalidQuery, "card", "20260101-x-task")
	if got := TriageError(structured); !strings.Contains(got, "20260101-x-task") {
		t.Fatalf("structured=%q", got)
	}
}
