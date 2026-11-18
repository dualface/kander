package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

const (
	secretText     = "SECRET-FINDING-TEXT-7c1"
	secretEvidence = "SECRET-EVIDENCE-7c1"
	secretBasis    = "SECRET-BASIS-7c1"
)

// useEnglish pins the language of the board messages; the lines of a refusal
// follow the language of the process.
func useEnglish(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLangCLI, "en")
}

// twoFindingFixture is a card in review with one successful PMQA run that holds
// two findings with distinctive prose, both assigned to the card.
func twoFindingFixture(t *testing.T) (string, DispatchInput, ReviewRun, []ReviewFinding) {
	t.Helper()
	useEnglish(t)
	root := tempBoard(t)
	id := gateCard(t, root, "problems")
	gatePlan(t, root, []string{id}, archiveRequirements())
	items := []ReviewFinding{
		{ID: "PM-01", Tier: "medium", Text: secretText + "-one", Evidence: secretEvidence + "-one"},
		{ID: "PM-02", Tier: "medium", Text: secretText + "-two", Evidence: secretEvidence + "-two"},
	}
	findings := emptyFindings()
	findings.Findings = items
	run := gateRun(t, root, archiveInput([]string{id}, "pm-problems", "PMQA"), findings)
	assignGate(t, root, run, map[string][]string{"PM-01": {id}, "PM-02": {id}})
	s := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	in := dispatchInput(s, "fix-problems")
	in.Kind, in.Base = "fix", run.Commit
	in.Evidence.Fix = &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{
		{FindingRef: FindingRef{run.RunID, "PM-01"}},
		{FindingRef: FindingRef{run.RunID, "PM-02"}},
	}}
	return root, in, run, items
}

// checkProposal runs the check of a new proposal the way the server does: in a
// write transaction on the scope FixProposalScope names, plus the task card.
func checkProposal(t *testing.T, root string, in DispatchInput) error {
	t.Helper()
	scope, err := FixProposalScope(root, in.Evidence.Fix)
	if err != nil {
		t.Fatal(err)
	}
	scope.Tasks = append(scope.Tasks, in.TaskID)
	return WithTransaction(root, scope, func(tx *Transaction) error { return CheckDispatchFixProposal(tx, in) })
}

func wantLines(t *testing.T, err error, want ...string) string {
	t.Helper()
	if err == nil {
		t.Fatal("evidence accepted")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != "board.dispatch_evidence_invalid" {
		t.Fatalf("not a dispatch evidence refusal: %v", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("refusal lacks %q:\n%v", w, err)
		}
	}
	return err.Error()
}

func TestFixProposalAcceptsValidEvidenceWithoutStamping(t *testing.T) {
	root, in, _, _ := twoFindingFixture(t)
	if err := checkProposal(t, root, in); err != nil {
		t.Fatal(err)
	}
	for _, f := range in.Evidence.Fix.Findings {
		if f.Section != "" {
			t.Fatal("the proposal check stamped the caller's binding")
		}
	}
}

func TestFixProposalNamesEachBatchAndFindingProblem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*DispatchInput, ReviewRun)
		want   []string
	}{
		{"batch missing", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.BatchID = "other" }, []string{`batch_id "other"`, "no such review batch"}},
		{"batch id invalid", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.BatchID = "../x" }, []string{`batch_id "../x"`}},
		{"base is not the target", func(in *DispatchInput, run ReviewRun) { in.Base = strings.Repeat("c", 40) }, []string{`base: provided "` + strings.Repeat("c", 40) + `"`, `"` + strings.Repeat("b", 40) + `"`, "batch batch "}},
		{"task is not a member", func(in *DispatchInput, _ ReviewRun) { in.TaskID = "20260908-foreign-task" }, []string{"task 20260908-foreign-task is not a member of batch batch (members: "}},
		{"no findings", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.Findings = nil }, []string{"findings: empty"}},
		{"duplicate finding", func(in *DispatchInput, run ReviewRun) {
			in.Evidence.Fix.Findings = append(in.Evidence.Fix.Findings, in.Evidence.Fix.Findings[0])
		}, []string{"pm-problems/PM-01: listed more than once"}},
		{"run unavailable", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.Findings[0].RunID = "missing" }, []string{"missing/PM-01: ", "successful run required"}},
		{"finding missing", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.Findings[0].FindingID = "PM-404" }, []string{"pm-problems/PM-404: the finding is not in the published report"}},
		{"wrong predecessor", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.Findings[0].PreviousRunID = "earlier" }, []string{`pm-problems/PM-01: previous_run_id: provided "earlier", the run record holds ""`}},
		{"section", func(in *DispatchInput, _ ReviewRun) { in.Evidence.Fix.Findings[0].Section = "NON_BLOCKING" }, []string{`pm-problems/PM-01: section: provided "NON_BLOCKING", the report holds "FINDINGS"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, in, run, _ := twoFindingFixture(t)
			tc.change(&in, run)
			wantLines(t, checkProposal(t, root, in), tc.want...)
		})
	}
}

func TestFixProposalNamesTheTaskOfAFindingNotAssignedToIt(t *testing.T) {
	root, in, run, _ := twoFindingFixture(t)
	other := gateCard(t, root, "other-member")
	in.TaskID = other
	text := wantLines(t, checkProposal(t, root, in), "pm-problems/PM-01: the finding is not assigned to task "+other, "(assigned to: ")
	if !strings.Contains(text, run.TaskIDs[0]) {
		t.Fatalf("the line does not name the task the finding is assigned to:\n%s", text)
	}
}

func TestFixRunProblemsNameBothValues(t *testing.T) {
	useEnglish(t)
	var run ReviewRun
	run.RunID, run.BatchID, run.Commit, run.PreviousRunID, run.TaskIDs = "r2", "other-batch", strings.Repeat("d", 40), "r1", []string{"20260908-one-task"}
	successor := func(id string) ReviewRun {
		var r ReviewRun
		r.RunID, r.PreviousRunID = id, "r2"
		return r
	}
	in := DispatchInput{TaskID: "20260908-two-task", Base: strings.Repeat("e", 40), Evidence: DispatchEvidence{Fix: &DispatchFixBinding{BatchID: "batch"}}}
	ref := DispatchFindingReference{FindingRef: FindingRef{"r2", "PM-01"}}
	targets := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	text := strings.Join(fixRunCheck{in: in, targets: targets}.problems(run, ref), "\n")
	for _, want := range []string{
		`r2/PM-01: the run belongs to batch "other-batch", but the evidence names batch "batch"`,
		"r2/PM-01: run commit " + run.Commit + " is not on the advance chain of the batch",
		strings.Repeat("a", 40) + ", " + strings.Repeat("b", 40),
		`r2/PM-01: previous_run_id: provided "", the run record holds "r1"`,
		"r2/PM-01: task 20260908-two-task is not in the task_ids of the run (20260908-one-task)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	run.Commit = targets[0]
	in.Base = strings.Repeat("e", 40)
	text = strings.Join(fixRunCheck{in: in, targets: targets, runs: []ReviewRun{successor("r3"), successor("r4")}}.problems(run, ref), "\n")
	for _, want := range []string{
		"r2/PM-01: base " + in.Base + " is not at or after run commit " + targets[0],
		"r2/PM-01: superseded review round; later run(s): r3, r4",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	// A completed dispatch replays history: later rounds are expected.
	if text = strings.Join(fixRunCheck{in: DispatchInput{TaskID: "20260908-one-task", Base: targets[1], Evidence: in.Evidence}, targets: targets, runs: []ReviewRun{successor("r3")}, completed: true}.problems(run, ref), "\n"); strings.Contains(text, "superseded") {
		t.Fatalf("history replay refused a later round:\n%s", text)
	}
}

func TestFixProposalListsEveryProblemInAFixedOrder(t *testing.T) {
	root, in, run, _ := twoFindingFixture(t)
	in.Base = strings.Repeat("c", 40)
	in.Evidence.Fix.Findings = []DispatchFindingReference{
		{FindingRef: FindingRef{run.RunID, "PM-01"}, PreviousRunID: "earlier"},
		{FindingRef: FindingRef{"missing", "PM-02"}},
		{FindingRef: FindingRef{run.RunID, "PM-01"}},
	}
	in.Evidence.Fix.Authors = []DispatchAuthorReference{
		{Finding: FindingRef{run.RunID, "PM-01"}, RecordID: "a", Author: "x", Artifact: ArtifactReference{TaskID: in.TaskID, Path: "../escape"}},
		{Finding: FindingRef{run.RunID, "PM-01"}, RecordID: "b", Author: "x", Artifact: ArtifactReference{TaskID: in.TaskID, Path: "reviews/x.json"}},
		{Finding: FindingRef{run.RunID, "PM-01"}, RecordID: "b", Author: "x", Artifact: ArtifactReference{TaskID: in.TaskID, Path: "reviews/x.json"}},
	}
	text := wantLines(t, checkProposal(t, root, in), "7 problem(s):")
	order := []string{
		"base: provided",
		"previous_run_id: provided",
		"missing/PM-02: ",
		"pm-problems/PM-01: listed more than once",
		"authors: reference " + run.RunID + "/PM-01/a has an invalid artifact",
		"authors: reference " + run.RunID + "/PM-01/b is listed more than once",
		"authors: the author originals were not compared with the expected set because 3 finding(s) failed above",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(text, want)
		if i < 0 || i < last {
			t.Fatalf("%q is missing or out of order in:\n%s", want, text)
		}
		last = i
	}
	if strings.Contains(text, "missing original") {
		t.Fatalf("a failed finding still reported author originals:\n%s", text)
	}
}

func TestFixProposalReportsAuthorReferenceProblems(t *testing.T) {
	root, in, run, items := twoFindingFixture(t)
	s := transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	var records []ReviewDisposition
	for _, f := range items {
		s = transactionSnapshot(t, root, in.TaskID)
		d := ReviewDisposition{RecordID: "author-" + strings.ToLower(f.ID), RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: secretBasis}
		if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
			t.Fatal(err)
		}
		records = append(records, d)
	}
	ref := func(d ReviewDisposition) DispatchAuthorReference {
		return DispatchAuthorReference{Finding: FindingRef{d.RunID, d.FindingID}, RecordID: d.RecordID, Author: d.Author, Artifact: ArtifactReference{in.TaskID, dispositionPath(d)}}
	}
	// Both originals are missing, then one is referenced with two wrong fields,
	// and a reference to an original that does not exist is not in the lineage.
	missing := func() string { return wantLines(t, checkProposal(t, root, in)) }
	first := missing()
	for _, d := range records {
		line := "authors: missing original finding.run_id=" + run.RunID + " finding.finding_id=" + d.FindingID + " record_id=" + d.RecordID + " author=codex artifact.path=" + dispositionPath(d)
		if !strings.Contains(first, line) {
			t.Fatalf("missing %q in:\n%s", line, first)
		}
	}
	if strings.Index(first, "finding_id=PM-01 ") > strings.Index(first, "finding_id=PM-02 ") {
		t.Fatalf("missing originals are not sorted:\n%s", first)
	}
	// The map the expected originals come from has no order; the text must not.
	for range 20 {
		if again := missing(); again != first {
			t.Fatalf("one board state gave two texts:\n%s\n---\n%s", first, again)
		}
	}
	wrong := ref(records[0])
	wrong.Author, wrong.Artifact.Path = "coordinator", "reviews/elsewhere.json"
	stranger := DispatchAuthorReference{Finding: FindingRef{run.RunID, "PM-01"}, RecordID: "no-such-record", Author: "codex", Artifact: ArtifactReference{in.TaskID, "reviews/no-such.json"}}
	in.Evidence.Fix.Authors = []DispatchAuthorReference{wrong, stranger}
	text := wantLines(t, checkProposal(t, root, in),
		"authors: reference "+run.RunID+"/PM-01/"+records[0].RecordID+": author: provided \"coordinator\", expected \"codex\"",
		"authors: reference "+run.RunID+"/PM-01/"+records[0].RecordID+": artifact.path: provided \"reviews/elsewhere.json\", expected \""+dispositionPath(records[0])+"\"",
		"authors: reference "+run.RunID+"/PM-01/no-such-record is not an original of the listed findings or their lineage",
		"finding_id=PM-02 record_id="+records[1].RecordID,
	)
	if strings.Contains(text, "finding_id=PM-01 record_id="+records[0].RecordID) {
		t.Fatalf("a referenced original was reported missing:\n%s", text)
	}
	in.Evidence.Fix.Authors = []DispatchAuthorReference{ref(records[0]), ref(records[1])}
	if err := checkProposal(t, root, in); err != nil {
		t.Fatalf("complete author references refused: %v", err)
	}
	// A refusal never carries finding prose or the basis of a record.
	in.Evidence.Fix.Authors = nil
	for _, leak := range []string{secretText, secretEvidence, secretBasis} {
		if strings.Contains(missing(), leak) {
			t.Fatalf("the refusal carries %q", leak)
		}
	}
}

func TestFixProposalOnASupersededRoundNamesTheLineageOriginals(t *testing.T) {
	useEnglish(t)
	root, in, run, f := fixBindingFixture(t)
	s := transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	record := ReviewDisposition{RecordID: "prior-author", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: secretBasis}
	if err := SubmitReviewDisposition(root, record, s.Revision); err != nil {
		t.Fatal(err)
	}
	nextInput := archiveInput([]string{in.TaskID}, "pm-next-round", "PMQA")
	nextInput.PreviousRunID, nextInput.ReviewedCommit = run.RunID, run.Commit
	next := emptyFindings()
	next.Findings = []ReviewFinding{{ID: "PM-02", Tier: "medium", Text: secretText, Evidence: secretEvidence, Lineage: &FindingRef{run.RunID, f.ID}}}
	nextRun := gateRun(t, root, nextInput, next)
	assignGate(t, root, nextRun, map[string][]string{"PM-02": {in.TaskID}})

	// The first round now has a successor, so a proposal on it names that run.
	wantLines(t, checkProposal(t, root, in), run.RunID+"/"+f.ID+": superseded review round; later run(s): "+nextRun.RunID)
	// A second-round finding that leaves previous_run_id out and forgets the
	// original of the earlier round gets both named.
	in.Evidence.Fix.Findings = []DispatchFindingReference{{FindingRef: FindingRef{nextRun.RunID, "PM-02"}}}
	wantLines(t, checkProposal(t, root, in),
		nextRun.RunID+`/PM-02: previous_run_id: provided "", the run record holds "`+run.RunID+`"`,
		"authors: the author originals were not compared with the expected set because 1 finding(s) failed above",
	)
	in.Evidence.Fix.Findings[0].PreviousRunID = run.RunID
	text := wantLines(t, checkProposal(t, root, in), "authors: missing original finding.run_id="+run.RunID+" finding.finding_id="+f.ID+" record_id=prior-author author=codex artifact.path="+dispositionPath(record))
	for _, leak := range []string{f.Text, secretText, secretEvidence, secretBasis} {
		if strings.Contains(text, leak) {
			t.Fatalf("the refusal carries %q", leak)
		}
	}
	// Prepare lists the same problems and never the sender's next step.
	_, err := PrepareDispatch(root, in)
	prepare := wantLines(t, err, "missing original")
	if next := config.Text("board.dispatch_problem_next_step"); strings.Contains(prepare, next) || !strings.Contains(text, next) {
		t.Fatalf("next step: prepare %v, proposal %v", strings.Contains(prepare, next), strings.Contains(text, next))
	}
}

// A batch that cannot be read is reported like any other problem, so the sender
// still gets a list, while a failure that says nothing about the evidence stays
// an error.
func TestFixProposalSeparatesEvidenceFailuresFromErrors(t *testing.T) {
	root, in, run, _ := twoFindingFixture(t)
	path := filepath.Join(control(root, "groups", reviewControlGroup), "batches", run.BatchID+".json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantLines(t, checkProposal(t, root, in), "1 problem(s):", "Invalid review evidence")
	// A record whose own batch_id differs from its name is a mismatch.
	if err = os.WriteFile(path, []byte(strings.Replace(string(good), `"batch_id": "`+run.BatchID+`"`, `"batch_id": "renamed"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	wantLines(t, checkProposal(t, root, in), `batch_id: provided "`+run.BatchID+`", the batch record holds "renamed"`)
	// A directory where the record belongs is an I/O failure, not a problem.
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	var e *Error
	if _, err = FixProposalScope(root, in.Evidence.Fix); err == nil || errors.As(err, &e) {
		t.Fatalf("the scope hid a failure of the store: %v", err)
	}
	scope := LockScope{Groups: []string{reviewControlGroup}, Tasks: []string{in.TaskID}}
	err = WithTransaction(root, scope, func(tx *Transaction) error { return CheckDispatchFixProposal(tx, in) })
	if err == nil || errors.As(err, &e) && e.Code == "board.dispatch_evidence_invalid" {
		t.Fatalf("a failure of the store was reported as a problem of the evidence: %v", err)
	}
}

func TestEvidenceProblemClassifiesErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"review evidence", reviewError("successful run required"), true},
		{"dispatch evidence", dispatchEvidenceError("fix binding required"), true},
		{"wrapped", fmt.Errorf("run-1: %w", reviewError("missing batch")), true},
		{"transaction conflict", kanbanError("board.transaction_conflict"), false},
		{"plain error", errors.New("read: input/output error"), false},
	} {
		if _, got := evidenceProblem(tc.err); got != tc.want {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
}

func TestFixProposalAndPrepareJudgeTheSame(t *testing.T) {
	root, in, _, _ := twoFindingFixture(t)
	for _, valid := range []bool{false, true} {
		if valid {
			in.Evidence.Fix.Findings = in.Evidence.Fix.Findings[:1]
		} else {
			in.Evidence.Fix.Findings[1].PreviousRunID = "earlier"
		}
		proposal := checkProposal(t, root, in)
		_, prepare := PrepareDispatch(root, in)
		if (proposal == nil) != (prepare == nil) {
			t.Fatalf("valid=%v: proposal %v, prepare %v", valid, proposal, prepare)
		}
	}
}

// The historical check of a completed fix lists its problems like any other
// check but never tells the sender how to send again. The list lands in the
// checkpoint as the reason of a blocked member, which stays valid JSON, and the
// error that reconcile returns shows its first line and says where the rest is.
func TestCoordinatorHistoryCheckListsProblemsWithoutANextStep(t *testing.T) {
	useEnglish(t)
	root, c, in, run, _ := coordinatorAdvancedFix(t)
	d, err := ReadDispatch(root, in.TaskID, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := control(root, "groups", reviewControlGroup, "runs", run.RunID, "sidecar.json")
	if err = os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	scope, err := dispatchEvidenceScope(root, d.Input)
	if err != nil {
		t.Fatal(err)
	}
	scope.ReadOnly = true
	err = WithTransaction(root, scope, func(tx *Transaction) error { return validateDispatchFix(tx, d.Input, false, true) })
	reason := wantLines(t, err, "2 problem(s):", run.RunID+"/PM-01: ", "missing sidecar")
	if strings.Contains(reason, config.Text("board.dispatch_problem_next_step")) {
		t.Fatalf("the history check tells the sender how to send again:\n%s", reason)
	}
	// The reason is the text of the error, kept as it is in the checkpoint.
	blocked := &CoordinatorBlockedError{Group: c.GroupID, Reason: blockReason(err)}
	shown := blocked.Error()
	if strings.Contains(shown, "\n") || !strings.Contains(shown, "2 problem(s):") || !strings.Contains(shown, "kander coordinator show "+c.GroupID) {
		t.Fatalf("the error shows more than the first line or lacks the pointer:\n%s", shown)
	}
	member := CoordinatorMember{Blocked: &CoordinatorBlock{Reason: blockReason(err)}}
	data, err := json.Marshal(CoordinatorCheckpoint{Members: map[string]CoordinatorMember{in.TaskID: member}})
	if err != nil {
		t.Fatal(err)
	}
	var back CoordinatorCheckpoint
	if err = json.Unmarshal(data, &back); err != nil || back.Members[in.TaskID].Blocked.Reason != reason {
		t.Fatalf("the reason did not survive JSON: %v", err)
	}
	if one := (&CoordinatorBlockedError{Group: c.GroupID, Reason: "one line"}).Error(); strings.Contains(one, "coordinator show") || !strings.Contains(one, "one line") {
		t.Fatalf("a one-line reason: %s", one)
	}
	// A blocked state that does not change writes no new revision of the checkpoint.
	request := coordinatorRequest(t, root, c)
	if _, err = ReconcileCoordinator(context.Background(), root, request, nil); err == nil {
		t.Fatal("the damaged original was accepted")
	}
	first, err := ReadCoordinatorCheckpoint(root, c.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, first), nil); err == nil {
		t.Fatal("the damaged original was accepted")
	}
	second, err := ReadCoordinatorCheckpoint(root, c.GroupID)
	if err != nil || second.Revision != first.Revision {
		t.Fatalf("a repeated block raised the checkpoint revision: %d -> %d %v", first.Revision, second.Revision, err)
	}
}
