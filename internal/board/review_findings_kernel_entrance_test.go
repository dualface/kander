package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/statemachine/reviewfindings"
)

// findingsStateOf is the kernel state of one run as the persisted records hold
// it, read the way the entrances project it.
func findingsStateOf(t *testing.T, root, runID string) reviewfindings.State {
	t.Helper()
	run, err := ReadReviewRun(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	var st reviewfindings.State
	err = WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error {
		report, _, err := tx.ReadGroup(reviewControlGroup, "runs/"+runID+"/originals/report.md")
		if err != nil {
			return err
		}
		facts, _, _ := reportFindingsFacts(report)
		var m ReviewInterpretation
		var legacy LegacyFindingMap
		var ledger dispositionLedger
		if facts.interp, err = readReviewJSON(tx, interpretationName(runID), &m); err != nil {
			return err
		}
		if facts.legacy, err = readReviewJSON(tx, "runs/"+runID+"/legacy-map.json", &legacy); err != nil {
			return err
		}
		if facts.assigned, err = readReviewJSON(tx, ledgerName(runID), &ledger); err != nil {
			return err
		}
		if facts.closed, err = reviewBatchClosed(tx, run); err != nil {
			return err
		}
		st = projectReviewFindings(run, facts)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// findingsInvariant is the invariant of formal/KanderFormal/Proofs/ReviewFindings.lean
// that a projected state must satisfy, so a projection never leaves the states the
// proofs cover.
func findingsInvariant(t *testing.T, st reviewfindings.State) {
	t.Helper()
	available := reviewfindings.Available(st, findingsParams)
	switch {
	case !st.Opened:
		t.Fatalf("a published run projects as read in: %+v", st)
	case st.Structured && !st.Fenced:
		t.Fatalf("a structured report holds the marker: %+v", st)
	case st.Interp && (st.Schema != reviewfindings.SchemaReceiver || st.Structured):
		t.Fatalf("an interpretation exists only for an unstructured receiver report: %+v", st)
	case st.Legacy && (st.Schema != reviewfindings.SchemaLegacy || st.Fenced):
		t.Fatalf("a legacy mapping exists only for an unmarked legacy report: %+v", st)
	case st.Interp && st.Legacy:
		t.Fatalf("a run has at most one source: %+v", st)
	case st.Assigned && !available:
		t.Fatalf("an assignment needs the findings: %+v", st)
	}
}

// refineFindings runs action, which must succeed, and asserts that the run now
// projects to the kernel step of its projection from before.
func refineFindings(t *testing.T, root, runID string, event reviewfindings.Event, action func() error) {
	t.Helper()
	before := findingsStateOf(t, root, runID)
	findingsInvariant(t, before)
	if err := action(); err != nil {
		t.Fatal(err)
	}
	want, ok := stepFindings(before, event)
	if !ok {
		t.Fatalf("the kernel refuses %+v on %+v", event, before)
	}
	after := findingsStateOf(t, root, runID)
	findingsInvariant(t, after)
	if after != want {
		t.Fatalf("projection after %+v, kernel step %+v", after, want)
	}
}

// refuseFindings runs action, which must fail with a message that contains want,
// and asserts that the kernel refuses the event and the projection is unchanged.
func refuseFindings(t *testing.T, root, runID string, event reviewfindings.Event, want string, action func() error) {
	t.Helper()
	before := findingsStateOf(t, root, runID)
	err := action()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want an error with %q, got %v", want, err)
	}
	if next, ok := stepFindings(before, event); ok || next != before {
		t.Fatalf("the kernel accepts %+v on %+v", event, before)
	}
	if after := findingsStateOf(t, root, runID); after != before {
		t.Fatalf("a refused entrance changed the run: %+v -> %+v", before, after)
	}
}

func okAssign() reviewfindings.Assign {
	return reviewfindings.Assign{Provenance: true, Lineage: true, Covers: true, Members: true, Active: true, Owners: true, Same: true}
}

func okSource() (reviewfindings.Interpret, reviewfindings.MapLegacy) {
	return reviewfindings.Interpret{Valid: true, Same: true, Lineage: true}, reviewfindings.MapLegacy{Valid: true, Same: true, Lineage: true}
}

func findingsAssignment(run ReviewRun, items map[string][]string) ReviewAssignment {
	return ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "assignment", Items: items}
}

func legacyMap(run ReviewRun) LegacyFindingMap {
	f := emptyFindings()
	f.NonBlocking = []ReviewFinding{{ID: "OLD-01", Tier: "suggest", Text: "PASS", Evidence: "report.md:1"}}
	return LegacyFindingMap{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "human", Basis: "mapping", Complete: true,
		Findings: f, Locations: []LegacyFindingLocation{{FindingID: "OLD-01", StartLine: 1, EndLine: 1, Quote: "PASS"}}}
}

// A structured run takes its findings from the report: neither an interpretation
// nor a legacy mapping can replace it (theorem 1), and its assignment refines the
// kernel, is replayed without change, and conflicts with a different one when it
// is already stored (theorems 2 and 4).
func TestStructuredRunEntrancesRefineFindingsKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "structured-a"), gateCard(t, root, "structured-b")}
	gatePlan(t, root, ids, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "finding", Evidence: "x.go:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput(ids, "structured", "PMQA"), findings)
	if st := findingsStateOf(t, root, run.RunID); !reviewfindings.Available(st, findingsParams) || reviewfindings.Pending(st, findingsParams) {
		t.Fatalf("a structured report has its findings: %+v", st)
	}
	interpret, mapLegacy := okSource()
	m := ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "reading", Complete: true, Findings: findings}
	refuseFindings(t, root, run.RunID, interpret, "provenance/completeness", func() error { return InterpretReview(root, m) })
	refuseFindings(t, root, run.RunID, mapLegacy, "cannot use legacy mapping", func() error {
		legacy := legacyMap(run)
		return MapLegacyReview(root, legacy)
	})
	assignment := findingsAssignment(run, map[string][]string{f.ID: {ids[0]}})
	for name, c := range map[string]struct {
		mutate func(*ReviewAssignment)
		fact   func(*reviewfindings.Assign)
		want   string
	}{
		"provenance": {func(a *ReviewAssignment) { a.Basis = "" }, func(e *reviewfindings.Assign) { e.Provenance = false }, "assignment provenance"},
		"coverage":   {func(a *ReviewAssignment) { a.Items = map[string][]string{} }, func(e *reviewfindings.Assign) { e.Covers = false }, "covering every finding"},
		"foreign":    {func(a *ReviewAssignment) { a.Items = map[string][]string{f.ID: {"20260101-outside-task"}} }, func(e *reviewfindings.Assign) { e.Members = false }, "foreign assignment"},
	} {
		t.Run(name, func(t *testing.T) {
			event := okAssign()
			c.fact(&event)
			bad := assignment
			c.mutate(&bad)
			refuseFindings(t, root, run.RunID, event, c.want, func() error { return AssignReviewFindings(root, bad) })
		})
	}
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, assignment) })
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, assignment) })
	other := findingsAssignment(run, map[string][]string{f.ID: {ids[1]}})
	conflict := okAssign()
	conflict.Same = false
	refuseFindings(t, root, run.RunID, conflict, "immutable assignment conflict", func() error { return AssignReviewFindings(root, other) })
}

// A schema 1 report takes neither source, and its assignment still goes through
// the kernel.
func TestReportSchemaRunTakesNoSource(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "schema-one")}
	gatePlan(t, root, ids, archiveRequirements())
	input := archiveInput(ids, "schema-one", "PMQA")
	input.FindingsSchema = 1
	run := gateRun(t, root, input, emptyFindings())
	st := findingsStateOf(t, root, run.RunID)
	if st.Schema != reviewfindings.SchemaReport || !reviewfindings.Available(st, findingsParams) {
		t.Fatalf("%+v", st)
	}
	interpret, mapLegacy := okSource()
	m := ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "reading", Complete: true, Findings: emptyFindings()}
	refuseFindings(t, root, run.RunID, interpret, "provenance/completeness", func() error { return InterpretReview(root, m) })
	refuseFindings(t, root, run.RunID, mapLegacy, "cannot use legacy mapping", func() error { return MapLegacyReview(root, legacyMap(run)) })
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{})) })
}

// A receiver run whose report has no findings block waits for its interpretation:
// it cannot be assigned (theorem 3), takes no legacy mapping (theorem 1), stores
// one interpretation that a replay does not change and a different one cannot
// replace (theorem 2), and after its batch closes only replays it (theorem 8).
func TestPendingReceiverRunEntrancesRefineFindingsKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "pending-a"), gateCard(t, root, "pending-b")}
	gatePlan(t, root, ids, archiveRequirements())
	run := receiverRun(t, root, archiveInput(ids, "pending", "PMQA"), "No findings in either section.\n")
	st := findingsStateOf(t, root, run.RunID)
	pending, err := ReviewNeedsInterpretation(root, run.RunID)
	if err != nil || !pending || !reviewfindings.Pending(st, findingsParams) || reviewfindings.Available(st, findingsParams) {
		t.Fatalf("pending %v %v %+v", pending, err, st)
	}
	interpret, mapLegacy := okSource()
	// Theorem 3: a pending run takes no assignment.
	refuseFindings(t, root, run.RunID, okAssign(), "needs review interpret", func() error {
		return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{}))
	})
	// Theorem 1: a receiver run takes no legacy mapping.
	refuseFindings(t, root, run.RunID, mapLegacy, "cannot use legacy mapping", func() error { return MapLegacyReview(root, legacyMap(run)) })
	bad := zeroInterpretation(run, ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: "No findings in either section."})
	bad.Complete = false
	invalid := interpret
	invalid.Valid = false
	refuseFindings(t, root, run.RunID, invalid, "provenance/completeness", func() error { return InterpretReview(root, bad) })
	m := zeroInterpretation(run, ReviewReportQuote{StartLine: 1, EndLine: 1, Quote: "No findings in either section."})
	refineFindings(t, root, run.RunID, interpret, func() error { return InterpretReview(root, m) })
	if pending, err = ReviewNeedsInterpretation(root, run.RunID); err != nil || pending {
		t.Fatalf("interpreted run still pending: %v %v", pending, err)
	}
	refineFindings(t, root, run.RunID, interpret, func() error { return InterpretReview(root, m) })
	changed := m
	changed.Author = "another-receiver"
	different := interpret
	different.Same = false
	refuseFindings(t, root, run.RunID, different, "immutable interpretation conflict", func() error { return InterpretReview(root, changed) })
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{})) })
	if _, err = gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	if st = findingsStateOf(t, root, run.RunID); !st.Closed {
		t.Fatalf("closed batch: %+v", st)
	}
	// Theorem 8: the replay of the stored interpretation is accepted and changes
	// nothing; the assignment, which is also stored, is refused.
	refineFindings(t, root, run.RunID, interpret, func() error { return InterpretReview(root, m) })
	refuseFindings(t, root, run.RunID, okAssign(), "batch already closed", func() error {
		return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{}))
	})
}

// A receiver run whose report holds a findings block has its findings and takes no
// interpretation (theorem 1): the kernel refuses a new interpretation for a
// structured report.
func TestStructuredReceiverRunTakesNoInterpretation(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "receiver-structured")}
	gatePlan(t, root, ids, archiveRequirements())
	report := "Review complete.\n```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```\nNo findings in either section.\n"
	run := receiverRun(t, root, archiveInput(ids, "receiver-structured", "PMQA"), report)
	st := findingsStateOf(t, root, run.RunID)
	if st.Schema != reviewfindings.SchemaReceiver || !st.Structured || reviewfindings.Pending(st, findingsParams) {
		t.Fatalf("%+v", st)
	}
	interpret, _ := okSource()
	m := zeroInterpretation(run, ReviewReportQuote{StartLine: 5, EndLine: 5, Quote: "No findings in either section."})
	refuseFindings(t, root, run.RunID, interpret, "structured findings already available", func() error { return InterpretReview(root, m) })
	if pending, err := ReviewNeedsInterpretation(root, run.RunID); err != nil || pending {
		t.Fatalf("a structured report is not pending: %v %v", pending, err)
	}
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{})) })
}

// A legacy run (schema 0) whose report has no findings block needs a mapping
// before it can be assigned; it takes no interpretation, and the stored mapping
// is replayed without change and cannot be replaced (theorems 1, 2, and 3).
func TestLegacyRunEntrancesRefineFindingsKernel(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "legacy-kernel")
	run := finalizedRun(t, root, archiveInput([]string{id}, "old", "PMQA"))
	publishRun(t, root, run.RunID)
	interpret, mapLegacy := okSource()
	if st := findingsStateOf(t, root, run.RunID); st.Schema != reviewfindings.SchemaLegacy || reviewfindings.Available(st, findingsParams) || reviewfindings.Pending(st, findingsParams) {
		t.Fatalf("an unmapped legacy report has no findings and is not pending: %+v", st)
	}
	findings := legacyMap(run).Findings
	assignment := findingsAssignment(run, map[string][]string{"OLD-01": {id}})
	refuseFindings(t, root, run.RunID, okAssign(), "legacy mapping required", func() error { return AssignReviewFindings(root, assignment) })
	m := ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "reading", Complete: true, Findings: findings}
	refuseFindings(t, root, run.RunID, interpret, "provenance/completeness", func() error { return InterpretReview(root, m) })
	refineFindings(t, root, run.RunID, mapLegacy, func() error { return MapLegacyReview(root, legacyMap(run)) })
	refineFindings(t, root, run.RunID, mapLegacy, func() error { return MapLegacyReview(root, legacyMap(run)) })
	changed := legacyMap(run)
	changed.Basis = "another mapping"
	different := mapLegacy
	different.Same = false
	refuseFindings(t, root, run.RunID, different, "immutable legacy map", func() error { return MapLegacyReview(root, changed) })
	refineFindings(t, root, run.RunID, okAssign(), func() error { return AssignReviewFindings(root, assignment) })
	if reflect.DeepEqual(findingsStateOf(t, root, run.RunID), reviewfindings.Initial(findingsParams)) {
		t.Fatal("the run projects as unread")
	}
}

// An assignment gives no finding to a member that left the batch or to a card
// without an owner (theorem 4).
func TestAssignmentFactsAreJudgedByTheKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "facts-stays"), gateCard(t, root, "facts-gone"), archiveCard(t, root, "facts-ownerless")}
	gatePlan(t, root, ids, archiveRequirements())
	run := gateRun(t, root, archiveInput(ids, "facts-run", "PMQA"), deadEndFindings())
	deadEndExit(t, root, ids[1], "trash")
	excluded := okAssign()
	excluded.Active = false
	refuseFindings(t, root, run.RunID, excluded, ids[1], func() error {
		return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{"PMQA-01": {ids[1]}}))
	})
	ownerless := okAssign()
	ownerless.Owners = false
	refuseFindings(t, root, run.RunID, ownerless, "owner required", func() error {
		return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{"PMQA-01": {ids[2]}}))
	})
	refineFindings(t, root, run.RunID, okAssign(), func() error {
		return AssignReviewFindings(root, findingsAssignment(run, map[string][]string{"PMQA-01": {ids[0]}}))
	})
}
