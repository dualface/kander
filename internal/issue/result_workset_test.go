package issue

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// multiProposal assesses the anchor's four criteria and one criterion per sibling.
func multiProposal(i ResultInspection) ResultProposal {
	p := fixtureProposal(i)
	for range i.Cards[1:] {
		p.Outcomes = append(p.Outcomes, "met")
	}
	return p
}

func TestResultRefusesIncompleteWorkSet(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	ctx := context.Background()
	inspection := inspectFixture(t, root, repo, id, fake)
	for _, state := range []string{"backlog", "todo", "working", "review"} {
		t.Run(state, func(t *testing.T) {
			pending := siblingFixture(t, root, "pending-"+state, sourceLine(key, id), state, "")
			defer moveFixture(t, root, pending, state, "trash", "trashed")
			old := triageStarter
			t.Cleanup(func() { triageStarter = old })
			triageStarter = func(TriageLaunch) (TriageOutcome, error) {
				t.Fatal("result session started for an incomplete work set")
				return TriageOutcome{}, nil
			}
			calls := map[string]func() error{
				"start":   func() error { _, err := StartResult(ctx, fake, root, repo, 42, TriageOptions{CardID: id}); return err },
				"inspect": func() error { _, err := InspectResult(ctx, fake, root, repo, 42, id); return err },
				"apply": func() error {
					_, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(inspection))
					return err
				},
				"decide": func() error {
					_, err := DecideResult(ctx, fake, root, repo, 42, id, ResultDecision{Token: inspection.Token, Decision: "yes", UserReference: "user said yes"})
					return err
				},
			}
			for action, call := range calls {
				if err := call(); err == nil || !strings.Contains(err.Error(), pending) {
					t.Fatalf("%s: err = %v", action, err)
				}
			}
		})
	}
	if fake.posts != 0 || fake.closes != 0 {
		t.Fatal("remote written for an incomplete work set")
	}
}

func TestResultRejectsSiblingCardArgument(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-arg", sourceLine(key, id), "done", "")
	_, err := InspectResult(context.Background(), fake, root, repo, 42, sibling)
	if err == nil || !strings.Contains(err.Error(), "--card "+id) {
		t.Fatalf("err = %v", err)
	}
}

func TestResultInspectListsEveryCardAndExclusion(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	a := siblingFixture(t, root, "sib-a", sourceLine(key, id), "done", "completed")
	b := siblingFixture(t, root, "sib-b", sourceLine(key, id), "archived", "completed")
	cancelled := siblingFixture(t, root, "sib-c", sourceLine(key, id), "archived", "cancelled")
	i := inspectFixture(t, root, repo, id, fake)
	got := []string{}
	for _, card := range i.Cards {
		if card.Spec == "" {
			t.Fatalf("card %s has no spec", card.TaskID)
		}
		got = append(got, card.TaskID+":"+card.State)
	}
	if !reflect.DeepEqual(got, []string{id + ":done", a + ":done", b + ":archived"}) || i.Card.TaskID != id {
		t.Fatalf("cards = %v anchor = %s", got, i.Card.TaskID)
	}
	if !reflect.DeepEqual(i.Excluded, []WorkCard{{TaskID: cancelled, State: "archived", Result: "cancelled"}}) {
		t.Fatalf("excluded = %+v", i.Excluded)
	}
}

func TestResultOutcomesFollowEveryCard(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-outcome", sourceLine(key, id), "done", "")
	ctx := context.Background()
	i := inspectFixture(t, root, repo, id, fake)
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i)); err == nil {
		t.Fatal("anchor-only outcomes accepted for a two-card work set")
	}
	record, err := ApplyResult(ctx, fake, root, repo, 42, id, multiProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record.TaskIDs, []string{id, sibling}) || fake.posts != 1 {
		t.Fatalf("record = %+v posts = %d", record, fake.posts)
	}
	// The stored multi-card record must load again under the store validation.
	if _, err := InspectResult(ctx, fake, root, repo, 42, id); err != nil {
		t.Fatal(err)
	}
}

func TestResultAcceptsEvidenceRecordedOnSibling(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	siblingFixture(t, root, "sib-evidence", sourceLine(key, id), "done", "")
	i := inspectFixture(t, root, repo, id, fake)
	p := multiProposal(i)
	p.Checks = []ResultCheck{{Command: "go vet ./...", Status: "pass"}}
	p.ResolutionEvidence = "Sibling sib-evidence verified: go vet ./..."
	if _, err := ApplyResult(context.Background(), fake, root, repo, 42, id, p); err != nil {
		t.Fatal(err)
	}
}

func TestResultRequiresReportOfLargeSibling(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-large", sourceLine(key, id), "done", "")
	spec := filepath.Join(root, "done", sibling, "spec.md")
	data, _ := os.ReadFile(spec)
	if err := os.WriteFile(spec, []byte(strings.Replace(string(data), "- SIZE: small", "- SIZE: large", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := InspectResult(context.Background(), fake, root, repo, 42, id)
	if err == nil || !strings.Contains(err.Error(), sibling) {
		t.Fatalf("err = %v", err)
	}
}

func TestResultIdentityChangesWithSiblingEvidence(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-change", sourceLine(key, id), "done", "")
	before, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	token := inspectFixture(t, root, repo, id, fake).Token
	spec := filepath.Join(root, "done", sibling, "spec.md")
	data, _ := os.ReadFile(spec)
	if err := os.WriteFile(spec, []byte(strings.Replace(string(data), "## SUMMARY\n", "## SUMMARY\n\nFollow-up delivered.\n", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.EvidenceVersion == before.EvidenceVersion || inspectFixture(t, root, repo, id, fake).Token == token {
		t.Fatal("sibling SUMMARY change kept the evidence version or token")
	}
}

// pinnedResultVersion was computed by the result key algorithm before work
// sets existed.
const pinnedResultVersion = "e83c6ac921ebd1838330fb5cbb0f26d2caf6dbd94371c5a2c2e13919175baaa1"

// Single-card identity must equal the pre-work-set formulas, so records
// written before this change stay valid.
func TestResultSingleCardIdentityUnchanged(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := board.SectionBody(snapshot.Text, "SUMMARY")
	acceptance, _ := board.SectionBody(snapshot.Text, "ACCEPTANCE_CRITERIA")
	work, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	if work.TaskKey != id || work.EvidenceVersion != resultHash([]string{summary, acceptance, ""}) || work.Digest != resultHash([]string{key, id, snapshot.Text, ""}) {
		t.Fatalf("single-card identity drifted: %+v", work)
	}
	if !reflect.DeepEqual(work.Commits, []string{strings.Repeat("a", 40)}) || work.taskIDs() != nil {
		t.Fatalf("commits = %v ids = %v", work.Commits, work.taskIDs())
	}
	record, err := ApplyResult(context.Background(), fake, root, repo, 42, id, fixtureProposal(inspectFixture(t, root, repo, id, fake)))
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != resultVersion(id, work.Commits, []string{"met", "met", "met", "met"}, true) || record.TaskIDs != nil {
		t.Fatalf("record = %+v", record)
	}
	// Pin the hash function itself on fixed input.
	if got := resultVersion("20260101-pin-task", []string{strings.Repeat("a", 40)}, []string{"met"}, true); got != pinnedResultVersion {
		t.Fatalf("resultVersion drifted: %s", got)
	}
}

func TestResultCompletedArchivedAnchor(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	moveFixture(t, root, id, "done", "archived", "completed")
	ctx := context.Background()
	i := inspectFixture(t, root, repo, id, fake)
	record, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	i = inspectFixture(t, root, repo, id, fake)
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, ResultDecision{Token: i.Token, Version: record.Version, Decision: "yes", UserReference: "user: close it"}); err != nil {
		t.Fatal(err)
	}
	if fake.closes != 1 {
		t.Fatal("completed archived anchor did not close")
	}
}

func TestResultRefusesWithdrawnArchivedAnchor(t *testing.T) {
	for _, result := range []string{"cancelled", ""} {
		root, repo, id, fake := resultFixture(t)
		moveFixture(t, root, id, "done", "archived", result)
		if _, err := InspectResult(context.Background(), fake, root, repo, 42, id); err == nil || !strings.Contains(err.Error(), id) {
			t.Fatalf("result %q: err = %v", result, err)
		}
	}
}

func setResultOnDone(t *testing.T, mode string) {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Issue.ResultOnDone = mode
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// handoffNotice runs the move observer around a fixture move and returns the notice.
func handoffNotice(t *testing.T, root, id, from, to, result string) string {
	t.Helper()
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	notice := observeResultHandoff(root, snapshot.Entry, to)
	moveFixture(t, root, id, from, to, result)
	if notice == nil {
		return ""
	}
	var out bytes.Buffer
	notice(&out, board.Entry{})
	return out.String()
}

func TestHandoffNoticeWhenLastCardCompletes(t *testing.T) {
	for _, mode := range []string{config.IssueResultAuto, config.IssueResultAsk} {
		t.Run(mode, func(t *testing.T) {
			root, repo, id, _ := resultFixture(t)
			key, _ := repo.IssueSourceKey(42)
			setResultOnDone(t, mode)
			sibling := siblingFixture(t, root, "sib-last", sourceLine(key, id), "working", "")
			text := handoffNotice(t, root, sibling, "working", "done", "completed")
			for _, want := range []string{key, id, "issue.result_on_done=" + mode, "kander issue result 42 --repo github.com/dualface/kander --card " + id + " --action inspect"} {
				if !strings.Contains(text, want) {
					t.Fatalf("notice missing %q:\n%s", want, text)
				}
			}
		})
	}
}

func TestHandoffNoticeWhenLastSiblingIsTrashed(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-dropped", sourceLine(key, id), "todo", "")
	if text := handoffNotice(t, root, sibling, "todo", "trash", "trashed"); !strings.Contains(text, "--card "+id) {
		t.Fatalf("notice missing:\n%s", text)
	}
}

func TestHandoffNoticeWhenLastCardArchivedCompleted(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	moveFixture(t, root, id, "done", "working", "")
	if text := handoffNotice(t, root, id, "working", "archived", "completed"); !strings.Contains(text, "--card "+id) {
		t.Fatalf("notice missing for %s:\n%s", key, text)
	}
}

func TestHandoffNoticeSilentWhenOff(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	setResultOnDone(t, config.IssueResultOff)
	sibling := siblingFixture(t, root, "sib-off", sourceLine(key, id), "working", "")
	if text := handoffNotice(t, root, sibling, "working", "done", "completed"); text != "" {
		t.Fatalf("notice printed with off:\n%s", text)
	}
}

func TestHandoffNoticeSilentWhileStillPending(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	first := siblingFixture(t, root, "sib-first", sourceLine(key, id), "working", "")
	siblingFixture(t, root, "sib-second", sourceLine(key, id), "working", "")
	if text := handoffNotice(t, root, first, "working", "done", "completed"); text != "" {
		t.Fatalf("notice printed while a sibling is pending:\n%s", text)
	}
}

func TestHandoffNoticeSilentWhenAlreadyReady(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	// A done sibling archived as completed keeps an already ready work set ready.
	sibling := siblingFixture(t, root, "sib-ready", sourceLine(key, id), "done", "completed")
	if text := handoffNotice(t, root, sibling, "done", "archived", "completed"); text != "" {
		t.Fatalf("notice printed for an already ready work set:\n%s", text)
	}
}

func TestHandoffNoticeSilentForUnrelatedCard(t *testing.T) {
	root, _, _, _ := resultFixture(t)
	other := siblingFixture(t, root, "unrelated", "PREREQUISITES: N/A", "working", "")
	if text := handoffNotice(t, root, other, "working", "done", "completed"); text != "" {
		t.Fatalf("notice printed for an unrelated card:\n%s", text)
	}
}

// The CLI path: the notice goes to stderr and the move output stays unchanged.
func TestHandoffNoticeThroughCLIMove(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	sibling := siblingFixture(t, root, "sib-cli", sourceLine(key, id), "todo", "")
	code, out, errText := captureMove(t, []string{sibling, "trash", "--result", "trashed", "--reason", "split no longer needed", "--decision", "test decision"})
	if code != 0 {
		t.Fatalf("move: %s", errText)
	}
	if out != filepath.Join(root, "trash", sibling)+"\n" {
		t.Fatalf("stdout changed: %q", out)
	}
	if !strings.Contains(errText, "--card "+id+" --action inspect") {
		t.Fatalf("stderr missing notice:\n%s", errText)
	}
}

func captureMove(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	code := board.RunMove(args)
	os.Stdout, os.Stderr = oldOut, oldErr
	outText, outErr := closeAndRead(outW, outR)
	errText, errErr := closeAndRead(errW, errR)
	if err := errors.Join(outErr, errErr); err != nil {
		t.Fatal(err)
	}
	return code, outText, errText
}

func closeAndRead(w *os.File, r *os.File) (string, error) {
	closeErr := w.Close()
	data, readErr := io.ReadAll(r)
	return string(data), errors.Join(closeErr, readErr, r.Close())
}
