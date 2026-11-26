package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/statemachine/reviewdisposition"
)

// pairStateOf is the kernel state of one finding and card as the ledger and the
// closure record hold them.
func pairStateOf(t *testing.T, root, runID, findingID, taskID string) reviewdisposition.State {
	t.Helper()
	run, err := ReadReviewRun(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	var st reviewdisposition.State
	err = WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error {
		var ledger dispositionLedger
		exists, err := readReviewJSON(tx, ledgerName(runID), &ledger)
		if err != nil {
			return err
		}
		if st.Closed, err = reviewBatchClosed(tx, run); err != nil {
			return err
		}
		if !exists {
			return nil
		}
		st.Assigned = containsID(ledger.Assignment.Items[findingID], taskID)
		for _, d := range ledger.Records {
			st.Chain = st.Chain || d.FindingID == findingID && d.TaskID == taskID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// ledgerRecords is the number of records the ledger of the run holds.
func ledgerRecords(t *testing.T, root, runID string) int {
	t.Helper()
	run, err := ReadReviewRun(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	err = WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error {
		var ledger dispositionLedger
		if _, err := readReviewJSON(tx, ledgerName(runID), &ledger); err != nil {
			return err
		}
		n = len(ledger.Records)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The invariant of formal/KanderFormal/Proofs/ReviewDisposition.lean: a pair has
// records only when it is assigned.
func pairInvariant(t *testing.T, st reviewdisposition.State) {
	t.Helper()
	if st.Chain && !st.Assigned {
		t.Fatalf("a record without an assignment: %+v", st)
	}
}

// submitFacts is the kernel event of an author record with every fact holding but
// the ones the caller changes.
func submitFacts(status reviewdisposition.Status, gate, security bool, dup reviewdisposition.Dup, ref reviewdisposition.Ref) reviewdisposition.Submit {
	return reviewdisposition.Submit{Status: status, Dup: dup, Ref: ref, Gate: gate, Security: security, Expect: true, Exec: true, Full: true,
		Current: true, Ledger: true, Available: true, Shape: true, AuthorOk: true, FieldsOk: true, Bound: true, EvidenceOk: true, WaiverOk: true}
}

func withdrawFacts(dup reviewdisposition.Dup, ref reviewdisposition.Ref) reviewdisposition.Withdraw {
	return reviewdisposition.Withdraw{Dup: dup, Ref: ref, Expect: true, Declared: true, Excluded: true, Named: true, Ledger: true,
		Available: true, Shape: true, AuthorOk: true, FieldsOk: true, Bound: true, EvidenceOk: true, WaiverOk: true}
}

// refinePair runs action, which must succeed, and asserts that the pair projects to
// the kernel step of its projection from before.
func refinePair(t *testing.T, root, runID, finding, task string, event reviewdisposition.Event, action func() error) {
	t.Helper()
	before := pairStateOf(t, root, runID, finding, task)
	pairInvariant(t, before)
	if err := action(); err != nil {
		t.Fatal(err)
	}
	want, ok := stepPair(before, event)
	if !ok {
		t.Fatalf("the kernel refuses %+v on %+v", event, before)
	}
	after := pairStateOf(t, root, runID, finding, task)
	pairInvariant(t, after)
	if after != want {
		t.Fatalf("projection after %+v, kernel step %+v", after, want)
	}
}

// refusePair runs action, which must fail with a message that contains want, and
// asserts that the kernel refuses the event, the pair is unchanged, and the ledger
// holds the same number of records.
func refusePair(t *testing.T, root, runID, finding, task string, event reviewdisposition.Event, want string, action func() error) {
	t.Helper()
	before, records := pairStateOf(t, root, runID, finding, task), ledgerRecords(t, root, runID)
	err := action()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want an error with %q, got %v", want, err)
	}
	if next, ok := stepPair(before, event); ok || next != before {
		t.Fatalf("the kernel accepts %+v on %+v", event, before)
	}
	if after := pairStateOf(t, root, runID, finding, task); after != before || ledgerRecords(t, root, runID) != records {
		t.Fatalf("a refused entrance changed the pair: %+v -> %+v", before, after)
	}
}

type recordFixture struct {
	root    string
	run     ReviewRun
	a, b, c string
	gate    ReviewFinding
	nonGate ReviewFinding
}

// recordOf is an author record of the fixture's run by card id.
func (f recordFixture) recordOf(record, id string, finding ReviewFinding, status string) ReviewDisposition {
	return ReviewDisposition{RecordID: record, RunID: f.run.RunID, FindingID: finding.ID, BatchID: f.run.BatchID, TaskID: id, Author: "codex",
		ReportHash: f.run.Hashes["report.md"], Original: finding.Text, Status: status, Basis: "verified against the original commit and tests"}
}

func (f recordFixture) submit(t *testing.T, d ReviewDisposition) error {
	t.Helper()
	return SubmitReviewDisposition(f.root, d, transactionSnapshot(t, f.root, d.TaskID).Revision)
}

// newRecordFixture is a run with a gate finding (shared by cards a, b, and c) and a
// finding below the gate (card a) whose assignment is not written yet.
func newRecordFixture(t *testing.T, role string) recordFixture {
	t.Helper()
	root := tempBoard(t)
	f := recordFixture{root: root, a: gateCard(t, root, "record-a"), b: gateCard(t, root, "record-b"), c: gateCard(t, root, "record-c")}
	f.gate = ReviewFinding{ID: "PM-01", Tier: "medium", Text: "gate finding", Evidence: "x.go:1"}
	f.nonGate = ReviewFinding{ID: "PM-02", Tier: "suggest", Text: "suggestion", Evidence: "x.go:2"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f.gate}
	findings.NonBlocking = []ReviewFinding{f.nonGate}
	ids := []string{f.a, f.b, f.c}
	requirements := archiveRequirements()
	if role == "Security" {
		requirements = noReviewRequirements()
		requirements["Security"] = "required"
	}
	gatePlan(t, root, ids, requirements)
	f.run = gateRun(t, root, archiveInput(ids, "records", role), findings)
	return f
}

func (f recordFixture) assign(t *testing.T) {
	t.Helper()
	assignGate(t, f.root, f.run, map[string][]string{f.gate.ID: {f.a, f.b, f.c}, f.nonGate.ID: {f.a}})
}

// The author records of a pair refine the disposition kernel: a run without an
// assignment and a card that was not assigned the finding take no record
// (theorems 3 and 5); the status rules of the tier and the waiver role refuse
// (theorem 5); a record names the last record of its chain, an identical record is
// replayed without change, and a different record with a stored identifier
// conflicts (theorem 6).
func TestAuthorRecordsRefineDispositionKernel(t *testing.T) {
	f := newRecordFixture(t, "PMQA")
	one := f.recordOf("one", f.a, f.gate, "rejected")
	noLedger := submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	noLedger.Ledger = false
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, noLedger, "assignment required", func() error { return f.submit(t, one) })
	f.assign(t)
	unassigned := f.recordOf("elsewhere", f.b, f.nonGate, "rejected")
	refusePair(t, f.root, f.run.RunID, f.nonGate.ID, f.b, submitFacts(reviewdisposition.StatusRejected, false, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		"not assigned finding", func() error { return f.submit(t, unassigned) })
	for name, c := range map[string]struct {
		record  ReviewDisposition
		finding ReviewFinding
		event   reviewdisposition.Submit
		want    string
	}{
		"gate deferred": {f.recordOf("deferred", f.a, f.gate, "deferred"), f.gate,
			submitFacts(reviewdisposition.StatusDeferred, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty), "must-fix cannot be deferred"},
		"non-gate confirmed": {f.recordOf("confirmed", f.a, f.nonGate, "confirmed"), f.nonGate,
			submitFacts(reviewdisposition.StatusConfirmed, false, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty), "non-blocking status"},
		"non-gate waived": {f.recordOf("waived", f.a, f.nonGate, "waived"), f.nonGate,
			submitFacts(reviewdisposition.StatusWaived, false, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty), "non-blocking status"},
		"status outside the set": {f.recordOf("delegated", f.a, f.gate, "delegated"), f.gate,
			submitFacts(reviewdisposition.StatusOther, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty), "disposition status"},
	} {
		t.Run(name, func(t *testing.T) {
			refusePair(t, f.root, f.run.RunID, c.finding.ID, f.a, c.event, c.want, func() error { return f.submit(t, c.record) })
		})
	}
	waiver := f.recordOf("waiver", f.a, f.gate, "waived")
	waiver.Waiver = &ReviewWaiver{Policy: "accepted-risk", Decision: "user decision"}
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, submitFacts(reviewdisposition.StatusWaived, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		"waiver only for applicable security role", func() error { return f.submit(t, waiver) })
	// Theorem 6: the chain of the pair.
	first := submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.a, first, func() error { return f.submit(t, one) })
	replay := first
	replay.Dup = reviewdisposition.DupSame
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.a, replay, func() error { return f.submit(t, one) })
	changed := one
	changed.Basis = "a different basis than the stored record"
	conflict := first
	conflict.Dup = reviewdisposition.DupConflict
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, conflict, "immutable author record", func() error { return f.submit(t, changed) })
	two := f.recordOf("two", f.a, f.gate, "rejected")
	two.PreviousRecordID = "one"
	second := submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefLast)
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.a, second, func() error { return f.submit(t, two) })
	stale := f.recordOf("three", f.a, f.gate, "rejected")
	stale.PreviousRecordID = "one"
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefOther),
		"predecessor CAS conflict", func() error { return f.submit(t, stale) })
	noPredecessor := f.recordOf("four", f.a, f.gate, "rejected")
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		"predecessor CAS conflict", func() error { return f.submit(t, noPredecessor) })
	wrongFirst := f.recordOf("five", f.b, f.gate, "rejected")
	wrongFirst.PreviousRecordID = "one"
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.b, submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefOther),
		"predecessor CAS conflict", func() error { return f.submit(t, wrongFirst) })
	other := f.recordOf("other", f.b, f.gate, "rejected")
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.b, first, func() error { return f.submit(t, other) })
	// A record below the gate may be deferred, and a gate record may be fixed.
	deferred := f.recordOf("below", f.a, f.nonGate, "deferred")
	refinePair(t, f.root, f.run.RunID, f.nonGate.ID, f.a, submitFacts(reviewdisposition.StatusDeferred, false, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		func() error { return f.submit(t, deferred) })
}

// A security role may waive a gate finding (theorem 5).
func TestSecurityRoleWaiverRefinesDispositionKernel(t *testing.T) {
	f := newRecordFixture(t, "Security")
	f.assign(t)
	waiver := f.recordOf("waiver", f.a, f.gate, "waived")
	waiver.Waiver = &ReviewWaiver{Policy: "accepted-risk", Decision: "user decision"}
	event := submitFacts(reviewdisposition.StatusWaived, true, true, reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.a, event, func() error { return f.submit(t, waiver) })
}

// The withdrawal of a finding needs the exclusion of its member (theorem 7), is
// replayed without change, and goes through the same chain as author records.
func TestWithdrawalRefinesDispositionKernel(t *testing.T) {
	f := newRecordFixture(t, "PMQA")
	f.assign(t)
	withdrawn := func(record string) ReviewDisposition {
		d := f.recordOf(record, f.c, f.gate, "withdrawn")
		d.Author = "coordinator"
		return d
	}
	live := withdrawn("withdrawn")
	live.Withdrawal = &ReviewWithdrawal{ExclusionID: "any", Decision: "decision-2"}
	notExcluded := withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	notExcluded.Excluded = false
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, notExcluded, "excluded member", func() error { return f.submit(t, live) })
	// The status withdrawn is never an author record.
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, submitFacts(reviewdisposition.StatusWithdrawn, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		"excluded member", func() error { return f.submit(t, live) })
	deadEndExit(t, f.root, f.c, "trash")
	exclusion, ok := deadEndExclusion(t, f.root, f.c)
	if !ok {
		t.Fatal("the exit wrote no exclusion")
	}
	wrong := withdrawn("withdrawn")
	wrong.Withdrawal = &ReviewWithdrawal{ExclusionID: "wrong", Decision: "decision-2"}
	unnamed := withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	unnamed.Named = false
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, unnamed, "must name the exclusion", func() error { return f.submit(t, wrong) })
	undeclared := withdrawn("withdrawn")
	undeclared.Withdrawal = nil
	declared := withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	declared.Declared = false
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, declared, "withdrawn requires withdrawal", func() error { return f.submit(t, undeclared) })
	noDecision := withdrawn("withdrawn")
	noDecision.Withdrawal = &ReviewWithdrawal{ExclusionID: exclusion.ExclusionID, Decision: " "}
	blank := withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty)
	blank.Bound = false
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, blank, "withdrawn requires withdrawal", func() error { return f.submit(t, noDecision) })
	d := withdrawn("withdrawn")
	d.Withdrawal = &ReviewWithdrawal{ExclusionID: exclusion.ExclusionID, Decision: "decision-2"}
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.c, withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty), func() error { return f.submit(t, d) })
	replay := withdrawFacts(reviewdisposition.DupSame, reviewdisposition.RefEmpty)
	refinePair(t, f.root, f.run.RunID, f.gate.ID, f.c, replay, func() error { return f.submit(t, d) })
	again := withdrawn("withdrawn-again")
	again.Withdrawal = d.Withdrawal
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.c, withdrawFacts(reviewdisposition.DupNone, reviewdisposition.RefEmpty), "predecessor CAS conflict",
		func() error { return f.submit(t, again) })
}

// Once the batch is closed no author record is accepted (theorem 8), and a ledger
// that repeats a record identifier is refused when it is read (theorem 6).
func TestClosedBatchAndRepeatedRecordRefuseDispositions(t *testing.T) {
	f := newRecordFixture(t, "PMQA")
	f.assign(t)
	ids := []string{f.a, f.b, f.c}
	for i, id := range ids {
		refinePair(t, f.root, f.run.RunID, f.gate.ID, id, submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
			func() error { return f.submit(t, f.recordOf("record-"+string(rune('a'+i)), id, f.gate, "rejected")) })
	}
	last := f.recordOf("record-nongate", f.a, f.nonGate, "rejected")
	refinePair(t, f.root, f.run.RunID, f.nonGate.ID, f.a, submitFacts(reviewdisposition.StatusRejected, false, false, reviewdisposition.DupNone, reviewdisposition.RefEmpty),
		func() error { return f.submit(t, last) })
	// A ledger that holds a record identifier twice is refused by the read.
	run, err := ReadReviewRun(f.root, f.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var stored dispositionLedger
	read := func(tx *Transaction) error { _, err := readReviewJSON(tx, ledgerName(run.RunID), &stored); return err }
	if err = WithTransaction(f.root, reviewScope(run.TaskIDs, true), read); err != nil {
		t.Fatal(err)
	}
	tampered := stored
	tampered.Records = append(append([]ReviewDisposition{}, stored.Records...), stored.Records[0])
	if err = WithTransaction(f.root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		return tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(tampered))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = PublishReviewDisposition(f.root, run.BatchID); err == nil || !strings.Contains(err.Error(), "author record lineage") {
		t.Fatalf("a repeated record identifier: %v", err)
	}
	repeated := submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupConflict, reviewdisposition.RefLast)
	if next, ok := stepPair(reviewdisposition.State{Assigned: true, Chain: true}, repeated); ok || next != (reviewdisposition.State{Assigned: true, Chain: true}) {
		t.Fatal("the kernel accepts a repeated record identifier")
	}
	if err = WithTransaction(f.root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		return tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(stored))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = gateClose(t, f.root, map[string]ReviewRoleConclusion{"PMQA": passRole(f.run)}); err != nil {
		t.Fatal(err)
	}
	if st := pairStateOf(t, f.root, f.run.RunID, f.gate.ID, f.a); !st.Closed {
		t.Fatalf("closed batch: %+v", st)
	}
	revision := f.recordOf("revised", f.a, f.gate, "rejected")
	revision.PreviousRecordID = "record-a"
	closed := submitFacts(reviewdisposition.StatusRejected, true, false, reviewdisposition.DupNone, reviewdisposition.RefLast)
	refusePair(t, f.root, f.run.RunID, f.gate.ID, f.a, closed, "batch already closed", func() error { return f.submit(t, revision) })
	if reflect.DeepEqual(stored, tampered) {
		t.Fatal("the tampered ledger equals the stored one")
	}
}
