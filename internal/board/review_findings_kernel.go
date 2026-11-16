package board

import (
	"fmt"
	"strings"

	"github.com/dualface/kander/internal/statemachine/reviewdisposition"
	"github.com/dualface/kander/internal/statemachine/reviewfindings"
)

// The decisions about where the findings of a review run come from, how they
// are assigned to the member cards, and which author records a card accepts are
// made by the generated kernels in internal/statemachine/reviewfindings (one
// instance per run) and internal/statemachine/reviewdisposition (one instance
// per finding and card); see formal/KanderFormal/Machines/ReviewFindings.lean,
// ReviewDisposition.lean, and docs/formal-verification.md. This file projects
// persisted records into kernel states and runs kernel events.
//
// Projection of a run (reviewfindings):
//   - Schema is the persisted FindingsSchema: Receiver for 2, Legacy for 0 or
//     less, Report for every other value.
//   - Structured is whether the report original parses as a structured findings
//     block and Fenced whether it contains the block marker; the entrance that
//     reads the report computes both.
//   - Interp and Legacy are whether the run holds an interpretation or a legacy
//     mapping, Assigned whether it holds the assignment ledger, and Closed
//     whether its batch has a closure record.
//   - The run is projected as read in (Opened) because every caller has passed
//     the consumption check of reviewRunForConsumption first.
//
// Projection of a pair (reviewdisposition): Assigned is whether the assignment
// gives the finding to the card, Chain whether the ledger holds a record for the
// pair, and Closed is false because reviewRunForMutation refused a closed batch
// before the pair is read.
//
// Integrity and text facts stay hand-written: report and record hashes, the text
// validations, lineage, the authorization of the executing card, and the fix and
// waiver evidence. An entrance computes them in the order it always has, returns
// the first failure, and hands the kernel the result. The kernel decides what
// depends on the state of the run or of the pair: the schema and source rules,
// whether the findings are available, whether the batch is open, whether a stored
// record is replayed or conflicts, the status rules, and the record chain.

var (
	findingsParams    = reviewfindings.Params{}
	dispositionParams = reviewdisposition.Params{}
)

// reviewFindingsMarker opens the structured findings block of a report.
const reviewFindingsMarker = "```kander-findings"

func findingsSchemaOf(run ReviewRun) reviewfindings.Schema {
	switch {
	case run.FindingsSchema == FindingsSchemaReceiver:
		return reviewfindings.SchemaReceiver
	case run.FindingsSchema > 0:
		return reviewfindings.SchemaReport
	}
	return reviewfindings.SchemaLegacy
}

// findingsFacts are the persisted facts a run state is projected from.
type findingsFacts struct {
	structured, fenced, interp, legacy, assigned, closed bool
}

// reportFindingsFacts are the facts of a report original: whether it parses as
// a structured findings block and whether it contains the block marker.
func reportFindingsFacts(report []byte) (facts findingsFacts, findings ReviewFindings, parseErr error) {
	findings, parseErr = ParseReviewFindings(report)
	facts.structured = parseErr == nil
	// A parsable block contains the marker, so the facts stay consistent.
	facts.fenced = facts.structured || strings.Contains(string(report), reviewFindingsMarker)
	return facts, findings, parseErr
}

// projectReviewFindings is the kernel state of run with the persisted facts.
func projectReviewFindings(run ReviewRun, facts findingsFacts) reviewfindings.State {
	st, _ := reviewfindings.Step(reviewfindings.Initial(findingsParams), reviewfindings.Open{
		ToSchema: findingsSchemaOf(run), Structured: facts.structured, Fenced: facts.fenced, Consumable: true}, findingsParams)
	st.Interp, st.Legacy, st.Assigned, st.Closed = facts.interp, facts.legacy, facts.assigned, facts.closed
	return st
}

func stepFindings(st reviewfindings.State, e reviewfindings.Event) (reviewfindings.State, bool) {
	return reviewfindings.Step(st, e, findingsParams)
}

// reviewBatchClosed reports whether the batch of the run has a closure record.
func reviewBatchClosed(tx *Transaction, run ReviewRun) (bool, error) {
	var closure ReviewClosure
	return readReviewJSON(tx, closureName(run.BatchID), &closure)
}

// reviewFindingsView is the kernel state of a run together with the findings of
// the source the state selects.
type reviewFindingsView struct {
	state    reviewfindings.State
	findings ReviewFindings
	parseErr error
}

// loadReviewFindings reads the report original and the stored source records of
// run and projects them. It returns the integrity errors of the reads in the
// order runFindings always has: the report original, then the interpretation of
// a receiver run, or the legacy mapping of a legacy run whose report does not
// parse. Whether the findings are available is the kernel's query.
func loadReviewFindings(tx *Transaction, run ReviewRun) (v reviewFindingsView, err error) {
	data, ok, err := tx.ReadGroup(reviewControlGroup, "runs/"+run.RunID+"/originals/report.md")
	if err != nil {
		return v, err
	}
	if !ok || ReviewDigest(data) != run.Hashes["report.md"] {
		return v, reviewError("missing report original")
	}
	var interpretation ReviewInterpretation
	var interpreted bool
	if run.FindingsSchema == FindingsSchemaReceiver {
		if interpretation, interpreted, err = readInterpretation(tx, run, data); err != nil {
			return v, err
		}
	}
	facts, findings, parseErr := reportFindingsFacts(data)
	facts.interp = interpreted
	var mapping LegacyFindingMap
	if findingsSchemaOf(run) == reviewfindings.SchemaLegacy && !facts.structured {
		exists, err := readReviewJSON(tx, "runs/"+run.RunID+"/legacy-map.json", &mapping)
		if err != nil {
			return v, err
		}
		if exists {
			if mapping.RunID != run.RunID {
				return v, reviewError("legacy map run")
			}
			if err = validateLegacyMap(mapping, data); err != nil {
				return v, err
			}
			facts.legacy = true
		}
	}
	v = reviewFindingsView{state: projectReviewFindings(run, facts), findings: findings, parseErr: parseErr}
	switch {
	case facts.interp:
		v.findings = interpretation.Findings
	case facts.legacy:
		v.findings = mapping.Findings
	}
	return v, nil
}

// unavailable is the error of a run whose findings cannot be obtained: a pending
// interpretation for a receiver run, otherwise the report's parse error.
func (v reviewFindingsView) unavailable() error {
	if reviewfindings.Pending(v.state, findingsParams) {
		return fmt.Errorf("%w: %w", errInterpretationPending, reviewError("report needs review interpret: "+v.parseErr.Error()))
	}
	return v.parseErr
}

// findingsAvailable is the kernel query: the findings of the run can be obtained.
func (v reviewFindingsView) available() bool {
	return reviewfindings.Available(v.state, findingsParams)
}

// pairState is the kernel state of one finding and card as the ledger holds it.
func pairState(a ReviewAssignment, findingID, taskID string, chain bool) reviewdisposition.State {
	return reviewdisposition.State{Assigned: containsID(a.Items[findingID], taskID), Chain: chain}
}

func stepPair(st reviewdisposition.State, e reviewdisposition.Event) (reviewdisposition.State, bool) {
	return reviewdisposition.Step(st, e, dispositionParams)
}

var kernelDispositionStatuses = map[string]reviewdisposition.Status{
	"confirmed": reviewdisposition.StatusConfirmed, "fixed": reviewdisposition.StatusFixed,
	"rejected": reviewdisposition.StatusRejected, "unverifiable": reviewdisposition.StatusUnverifiable,
	"waived": reviewdisposition.StatusWaived, "deferred": reviewdisposition.StatusDeferred,
	"withdrawn": reviewdisposition.StatusWithdrawn,
}

func kernelDispositionStatus(status string) reviewdisposition.Status {
	if s, ok := kernelDispositionStatuses[status]; ok {
		return s
	}
	return reviewdisposition.StatusOther
}

// securityReviewRole reports the roles whose findings may be waived.
func securityReviewRole(role string) bool {
	return role == "CSA" || role == "Hacker" || role == "Security"
}

// recordEvent is the kernel event of presenting d to a pair with every fact the
// entrance already verified holding: an author record, or a withdrawal for the
// status withdrawn. dup and ref are the relation of d to the ledger.
func recordEvent(d ReviewDisposition, gate, security bool, dup reviewdisposition.Dup, ref reviewdisposition.Ref) reviewdisposition.Event {
	if d.Status == "withdrawn" {
		return reviewdisposition.Withdraw{Dup: dup, Ref: ref, Expect: true, Declared: true, Excluded: true, Named: true,
			Ledger: true, Available: true, Shape: true, AuthorOk: true, FieldsOk: true, Bound: true, EvidenceOk: true, WaiverOk: true}
	}
	return reviewdisposition.Submit{Status: kernelDispositionStatus(d.Status), Dup: dup, Ref: ref, Gate: gate, Security: security,
		Expect: true, Exec: true, Full: true, Current: true, Ledger: true, Available: true, Shape: true, AuthorOk: true,
		FieldsOk: true, Bound: true, EvidenceOk: true, WaiverOk: true}
}

// chainRef is the relation of a presented previous_record_id to the chain whose
// last record is last (empty when the chain has no record).
func chainRef(previousRecordID, last string) reviewdisposition.Ref {
	switch {
	case previousRecordID == "":
		return reviewdisposition.RefEmpty
	case previousRecordID == last:
		return reviewdisposition.RefLast
	}
	return reviewdisposition.RefOther
}
