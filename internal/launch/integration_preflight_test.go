package launch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestIntegrationPreflightBeforePublishingDivergedDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher fixture")
	}
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "merge-candidate")
	startThenReview(t, root, "claude", task, path)
	cwd, _ := integrationGit(t)
	git := func(args ...string) string {
		return gitIn(t, cwd, append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	}
	write := func(content, message string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, "file"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		git("add", "file")
		git("commit", "-qm", message)
		return git("rev-parse", "HEAD")
	}
	text := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	base := write(text, "base")
	git("switch", "-qc", "group/topic")
	delivery := write(strings.Replace(text, "five", "FIVE", 1), "delivery")
	in := closedWrapUpRange(t, root, task, cwd, base, delivery)
	c := claimFixtureGroup(t, root, task)
	git("switch", "-q", "develop")
	upstream := write(strings.Replace(text, "seven", "SEVEN", 1), "upstream")
	git("switch", "-qc", "candidate", "group/topic")
	git("rebase", "develop")
	g := &in.Evidence.WrapUp.Git
	g.SourceCommit = git("rev-parse", "HEAD")
	g.TargetCommit, g.RebasedBase = g.SourceCommit, upstream
	file := filepath.Join(t.TempDir(), "preflight.json")
	preflight := func() int {
		t.Helper()
		checked := *in.Evidence.WrapUp
		checked.Git.ReviewBase, checked.Git.ReviewTarget = "", ""
		request := board.DispatchInput{Kind: "check-integration", ID: in.ID, TaskID: task, Evidence: board.DispatchEvidence{WrapUp: &checked}}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return RunDispatch([]string{"prepare", file})
	}
	if preflight() == 0 {
		t.Fatal("accepted a rebase that changed patch context")
	}
	git("switch", "-qc", "merge-result", "develop")
	git("merge", "--no-edit", "group/topic")
	g.SourceCommit, g.TargetCommit, g.RebasedBase = delivery, git("rev-parse", "HEAD"), ""
	if preflight() != 0 {
		t.Fatal("refused a merge preserving both histories")
	}
	if git("rev-parse", "develop") != upstream {
		t.Fatal("preflight published the candidate")
	}
	if _, err := board.ReadDispatch(root, task, in.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected dispatch after preflight: %v", err)
	}
	if _, err := PrepareBoundDispatch(root, in); err == nil {
		t.Fatal("preflight satisfied post-publication check")
	}
	git("switch", "-q", "develop")
	git("commit", "--allow-empty", "-qm", "concurrent advance")
	if preflight() == 0 {
		t.Fatal("stale candidate passed after target advanced")
	}
	git("switch", "-q", "merge-result")
	git("merge", "--no-edit", "develop")
	g.TargetCommit = git("rev-parse", "HEAD")
	if preflight() != 0 {
		t.Fatal("refused rebuilt candidate")
	}
	git("switch", "-q", "develop")
	git("merge", "merge-result")
	launchFinishWrapUp(t, root, task, in, c)
	snapshot, err := board.ReadSnapshot(root, task)
	if err != nil || snapshot.Entry.State != "done" {
		t.Fatalf("expected completed member: %s, %v", snapshot.Entry.State, err)
	}
}
