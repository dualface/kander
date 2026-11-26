package board

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/statemachine/reviewdisposition"
	"github.com/dualface/kander/v2/internal/statemachine/reviewfindings"
)

// ReviewAssignment attributes every parsed item explicitly, including shared findings.
type ReviewAssignment struct {
	RunID      string              `json:"run_id"`
	BatchID    string              `json:"batch_id"`
	Author     string              `json:"author"`
	Basis      string              `json:"basis"`
	Items      map[string][]string `json:"items"`
	Owners     map[string]string   `json:"owners"`
	RecordedAt string              `json:"recorded_at"`
}
type ReviewWaiver struct {
	Policy    string `json:"policy"`
	Decision  string `json:"decision"`
	SentAt    string `json:"sent_at,omitempty"`
	TimeoutAt string `json:"timeout_at,omitempty"`
}

// ReviewDisposition is an immutable original author's observation. Revisions append
// using PreviousRecordID; the previous original remains available and attributable.
type ReviewDisposition struct {
	Authorization     *ExecutionAuthorization `json:"authorization,omitempty"`
	SubmittedRevision uint64                  `json:"submitted_revision,omitempty"`
	RecordID          string                  `json:"record_id"`
	PreviousRecordID  string                  `json:"previous_record_id,omitempty"`
	RunID             string                  `json:"run_id"`
	FindingID         string                  `json:"finding_id"`
	BatchID           string                  `json:"batch_id"`
	TaskID            string                  `json:"task_id"`
	Author            string                  `json:"author"`
	RecordedAt        string                  `json:"recorded_at"`
	ReportHash        string                  `json:"report_hash"`
	Original          string                  `json:"original"`
	Status            string                  `json:"status"`
	Basis             string                  `json:"basis"`
	FixCommit         string                  `json:"fix_commit,omitempty"`
	Mechanical        string                  `json:"mechanical,omitempty"`
	Verification      string                  `json:"verification,omitempty"`
	Waiver            *ReviewWaiver           `json:"waiver,omitempty"`
	Withdrawal        *ReviewWithdrawal       `json:"withdrawal,omitempty"`
}

// ReviewWithdrawal binds a withdrawn record to the exclusion of its member and to
// the user decision. It is never a fix, and only an excluded member can have one.
type ReviewWithdrawal struct {
	ExclusionID string `json:"exclusion_id"`
	Decision    string `json:"decision"`
}
type dispositionLedger struct {
	Assignment ReviewAssignment    `json:"assignment"`
	Records    []ReviewDisposition `json:"records"`
}

func ledgerName(run string) string { return "runs/" + run + "/dispositions.json" }
func dispositionPath(d ReviewDisposition) string {
	return "reviews/" + d.RunID + "/dispositions/" + d.RecordID + ".json"
}
func dispositionKey(d ReviewDisposition) string { return d.FindingID + "/" + d.TaskID }

// runFindingsView is the kernel state of the run with the findings of its
// source. A run whose findings are not available returns the pending or parse
// error with the run's ID in front.
func runFindingsView(tx *Transaction, run ReviewRun) (v reviewFindingsView, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", run.RunID, err)
		}
	}()
	if v, err = loadReviewFindings(tx, run); err != nil {
		return reviewFindingsView{}, err
	}
	if !v.available() {
		return v, v.unavailable()
	}
	return v, nil
}

func runFindings(tx *Transaction, run ReviewRun) (ReviewFindings, error) {
	v, err := runFindingsView(tx, run)
	return v.findings, err
}

// reviewRunForConsumption validates the successful immutable run and every
// publication without requiring its batch to remain open for new mutations.
func reviewRunForConsumption(tx *Transaction, runID string) (run ReviewRun, err error) {
	ok, err := readReviewJSON(tx, reviewRunName(runID), &run)
	if err != nil {
		return
	}
	if !ok || run.RunID != runID || run.ExecutionStatus != "ok" {
		return run, reviewError("successful run required")
	}
	err = verifyPublishedReview(tx, run)
	return
}

// reviewRunForMutation is the consumption check plus the kernel's decision that
// the batch of the run is still open.
func reviewRunForMutation(tx *Transaction, runID string) (run ReviewRun, err error) {
	run, err = reviewRunForConsumption(tx, runID)
	if err != nil {
		return
	}
	closed, err := reviewBatchClosed(tx, run)
	if err == nil && !reviewfindings.Writable(projectReviewFindings(run, findingsFacts{closed: closed}), findingsParams) {
		err = reviewError("batch already closed")
	}
	return
}
func MapLegacyReviewWithTransaction(root string, m LegacyFindingMap, extensions ...TransactionExtension) error {
	run, err := ReadReviewRun(root, m.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, m.RunID)
		if err != nil {
			return err
		}
		if !reviewfindings.LegacySchema(projectReviewFindings(run, findingsFacts{}), findingsParams) {
			return reviewError("new reports cannot use legacy mapping")
		}
		data, _, err := tx.ReadGroup(reviewControlGroup, "runs/"+m.RunID+"/originals/report.md")
		if err != nil {
			return err
		}
		if err = validateLegacyMap(m, data); err != nil {
			return err
		}
		if err = validateFindingLineage(tx, run, m.Findings); err != nil {
			return err
		}
		name := "runs/" + m.RunID + "/legacy-map.json"
		var old LegacyFindingMap
		exists, err := readReviewJSON(tx, name, &old)
		if err != nil {
			return err
		}
		if exists {
			m.RecordedAt = old.RecordedAt
		}
		facts, _, _ := reportFindingsFacts(data)
		facts.legacy = exists
		event := reviewfindings.MapLegacy{Valid: true, Same: !exists || reflect.DeepEqual(old, m), Lineage: true}
		if _, ok := stepFindings(projectReviewFindings(run, facts), event); !ok {
			if exists {
				return reviewError("immutable legacy map")
			}
			return reviewError("legacy mapping refused")
		}
		if exists {
			return nil
		}
		m.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		targets, err := reviewCopyTargets(tx, run.TaskIDs)
		if err != nil {
			return err
		}
		for _, id := range targets {
			if err = tx.Put(id, "reviews/"+m.RunID+"/legacy-map.json", reviewJSON(m)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, name, reviewJSON(m))
	}, extensions...)
}
func AssignReviewFindingsWithTransaction(root string, a ReviewAssignment, extensions ...TransactionExtension) error {
	run, err := ReadReviewRun(root, a.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, a.RunID)
		if err != nil {
			return err
		}
		if a.BatchID != run.BatchID || strings.TrimSpace(a.Author) == "" || strings.TrimSpace(a.Basis) == "" || a.Items == nil {
			return reviewError("assignment provenance")
		}
		v, err := runFindingsView(tx, run)
		if err != nil {
			return err
		}
		f := v.findings
		if err = validateFindingLineage(tx, run, f); err != nil {
			return err
		}
		// Each fact is judged by the kernel with every other fact holding, in the
		// order the entrance always checked them; the provenance came first.
		facts := reviewfindings.Assign{Provenance: true, Lineage: true, Covers: true, Members: true, Active: true, Owners: true, Same: true}
		allows := func(e reviewfindings.Assign) bool { _, ok := stepFindings(v.state, e); return ok }
		covers := facts
		covers.Covers = len(a.Items) == len(f.all())
		if !allows(covers) {
			return reviewError("assignment items must use finding IDs as keys and task IDs as values, covering every finding")
		}
		a.Owners = map[string]string{}
		for _, item := range f.all() {
			ids, err := normalizedReviewTasks(a.Items[item.ID])
			if err != nil {
				return err
			}
			a.Items[item.ID] = ids
			for _, id := range ids {
				member := facts
				member.Members = containsID(run.TaskIDs, id)
				if !allows(member) {
					return reviewError("foreign assignment")
				}
				excluded, err := memberExcluded(tx, id)
				if err != nil {
					return err
				}
				active := facts
				active.Active = !excluded
				if !allows(active) {
					return reviewError("assigned card left the plan: " + id)
				}
				s, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				owner := MetadataFrom(s.Text, FieldOwner)
				owned := facts
				owned.Owners = strings.TrimSpace(owner) != ""
				if !allows(owned) {
					return reviewError("assigned card owner required")
				}
				a.Owners[id] = owner
			}
		}
		var old dispositionLedger
		exists, err := readReviewJSON(tx, ledgerName(a.RunID), &old)
		if err != nil {
			return err
		}
		if exists {
			a.RecordedAt = old.Assignment.RecordedAt
		}
		v.state.Assigned = exists
		facts.Same = !exists || reflect.DeepEqual(a, old.Assignment)
		if !allows(facts) {
			if exists {
				return reviewError("immutable assignment conflict")
			}
			return reviewError("assignment refused")
		}
		if exists {
			return nil
		}
		a.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		targets, err := reviewCopyTargets(tx, run.TaskIDs)
		if err != nil {
			return err
		}
		for _, id := range targets {
			if err = tx.Put(id, "reviews/"+run.RunID+"/assignment.json", reviewJSON(a)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(dispositionLedger{Assignment: a, Records: []ReviewDisposition{}}))
	}, extensions...)
}

// validateDisposition validates d against its run, finding, and assignment. The
// facts that depend on the pair (the assignment, the status rules for the tier,
// and the role of a waiver) are judged by the disposition kernel with every other
// fact holding and the record chain empty, in the order the checks have always
// run; the text and evidence facts stay hand-written.
func validateDisposition(d ReviewDisposition, run ReviewRun, item ReviewFinding, a ReviewAssignment) error {
	pair := pairState(a, item.ID, d.TaskID, false)
	gate, security, withdrawn := mustFix(item.Tier), securityReviewRole(run.Role), d.Status == "withdrawn"
	allows := func(status reviewdisposition.Status, gate, security bool) bool {
		e := recordEvent(d, gate, security, reviewdisposition.DupNone, reviewdisposition.RefEmpty)
		if submit, ok := e.(reviewdisposition.Submit); ok {
			submit.Status = status
			e = submit
		}
		_, ok := stepPair(pair, e)
		return ok
	}
	switch {
	case !ValidReviewID(d.RecordID):
		return reviewError("disposition record_id must start with a lowercase letter or digit and then use only [a-z0-9-]")
	case d.RunID != run.RunID:
		return reviewError("disposition run_id must equal the review run")
	case d.BatchID != run.BatchID:
		return reviewError("disposition batch_id must equal the review run batch")
	case d.FindingID != item.ID:
		return reviewError("disposition finding_id must equal the finding")
	case !allows(reviewdisposition.StatusConfirmed, true, true):
		return reviewError("disposition task_id is not assigned finding " + d.FindingID)
	case d.Author != a.Owners[d.TaskID] && d.SubmittedRevision == 0:
		return reviewError(fmt.Sprintf("disposition author %q is not assignment owner %q for task %s", d.Author, a.Owners[d.TaskID], d.TaskID))
	case d.Author == "":
		return reviewError("disposition author is required")
	}
	switch {
	case d.ReportHash != run.Hashes["report.md"]:
		return reviewError("disposition report_hash must equal the SHA-256 of report.md")
	case d.Original != item.Text:
		return reviewError("disposition original must equal the finding text")
	case strings.TrimSpace(d.Basis) == "":
		return reviewError("disposition basis is required")
	}
	status := kernelDispositionStatus(d.Status)
	if status == reviewdisposition.StatusOther {
		return reviewError("disposition status")
	}
	if !gate && !withdrawn && !allows(status, false, true) {
		return reviewError("non-blocking status")
	}
	if withdrawn {
		if d.Withdrawal == nil || strings.TrimSpace(d.Withdrawal.ExclusionID) == "" || strings.TrimSpace(d.Withdrawal.Decision) == "" {
			return reviewError("withdrawn requires withdrawal.exclusion_id and withdrawal.decision")
		}
	} else if d.Withdrawal != nil {
		return reviewError("withdrawal without withdrawn status")
	}
	if gate && !withdrawn && !allows(status, true, true) {
		return reviewError("must-fix cannot be deferred")
	}
	if d.Status == "fixed" && !validCommit(d.FixCommit) {
		return reviewError("fixed requires fix_commit as a 40 or 64 character lowercase hex commit")
	}
	if d.Status == "fixed" && strings.TrimSpace(d.Verification) == "" {
		return reviewError("fixed requires verification")
	}
	if d.Status != "fixed" && (d.FixCommit != "" || d.Mechanical != "") {
		return reviewError("fix evidence without fixed status")
	}
	if d.Mechanical != "" && !mechanicalCategory(d.Mechanical) {
		return reviewError("mechanical category")
	}
	if d.Status == "waived" {
		if !allows(status, true, security) || d.Waiver == nil || strings.TrimSpace(d.Waiver.Decision) == "" {
			return reviewError("waiver only for applicable security role decisions")
		}
		if d.Waiver.Policy != "accepted-risk" && d.Waiver.Policy != "timed-out" {
			return reviewError("waiver policy")
		}
		if d.Waiver.Policy == "timed-out" {
			sent, e1 := time.Parse(time.RFC3339Nano, d.Waiver.SentAt)
			end, e2 := time.Parse(time.RFC3339Nano, d.Waiver.TimeoutAt)
			if e1 != nil || e2 != nil || end.Sub(sent) < 15*time.Minute || end.After(time.Now()) {
				return reviewError("security timeout evidence")
			}
		}
	} else if d.Waiver != nil {
		return reviewError("waiver without waived status")
	}
	return nil
}
func SubmitReviewDispositionWithTransaction(root string, d ReviewDisposition, expectedRevision uint64, extensions ...TransactionExtension) error {
	run, err := ReadReviewRun(root, d.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, d.RunID)
		if err != nil {
			return err
		}
		if d.Status == "withdrawn" {
			return withdrawReviewFinding(tx, run, d, expectedRevision)
		}
		s, err := tx.Expect(d.TaskID, "working", expectedRevision)
		if err != nil {
			return err
		}
		authorization := ExecutionAuthorization{}
		if d.Authorization != nil {
			authorization = *d.Authorization
		}
		if err = tx.requireExecution(s, authorization, true); err != nil {
			return err
		}
		if err = tx.requireFullExecution(s); err != nil {
			return err
		}
		if MetadataFrom(s.Text, FieldOwner) != d.Author {
			return reviewError("only current executing owner may submit")
		}
		d.SubmittedRevision = s.Revision
		ledger, fresh, err := appendDisposition(tx, run, &d)
		if err != nil || !fresh {
			return err
		}
		if err = tx.Put(d.TaskID, dispositionPath(d), reviewJSON(d)); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, ledgerName(d.RunID), reviewJSON(ledger))
	}, extensions...)
}

// appendDisposition validates d against the assignment and adds it to the ledger.
// fresh is false when the same record already exists. The disposition kernel
// decides whether d is that same record, a conflicting one, or a new record that
// names the last record of its chain.
func appendDisposition(tx *Transaction, run ReviewRun, d *ReviewDisposition) (ledger dispositionLedger, fresh bool, err error) {
	exists, err := readReviewJSON(tx, ledgerName(d.RunID), &ledger)
	if err != nil {
		return ledger, false, err
	}
	if !exists {
		return ledger, false, reviewError("assignment required")
	}
	findings, err := runFindings(tx, run)
	if err != nil {
		return ledger, false, err
	}
	var item ReviewFinding
	for _, f := range findings.all() {
		if f.ID == d.FindingID {
			item = f
		}
	}
	last := ""
	for _, record := range ledger.Records {
		if dispositionKey(record) == dispositionKey(*d) {
			last = record.RecordID
		}
	}
	if err = validateDisposition(*d, run, item, ledger.Assignment); err != nil {
		return ledger, false, err
	}
	dup := reviewdisposition.DupNone
	for _, record := range ledger.Records {
		if record.RecordID == d.RecordID {
			d.RecordedAt = record.RecordedAt
			d.SubmittedRevision = record.SubmittedRevision
			dup = reviewdisposition.DupConflict
			if reflect.DeepEqual(*d, record) {
				dup = reviewdisposition.DupSame
			}
			break
		}
	}
	event := recordEvent(*d, mustFix(item.Tier), securityReviewRole(run.Role), dup, chainRef(d.PreviousRecordID, last))
	if _, ok := stepPair(pairState(ledger.Assignment, d.FindingID, d.TaskID, last != ""), event); !ok {
		if dup == reviewdisposition.DupConflict {
			return ledger, false, reviewError("immutable author record")
		}
		return ledger, false, dispositionRefusal(*d, last)
	}
	if dup == reviewdisposition.DupSame {
		return ledger, false, nil
	}
	// Historical waivers remain readable and replayable under their original rules.
	if w := d.Waiver; w != nil && (w.Policy != "accepted-risk" || w.SentAt != "" || w.TimeoutAt != "") {
		return ledger, false, reviewError("new waivers require accepted-risk and omit legacy timestamps")
	}
	d.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	ledger.Records = append(ledger.Records, *d)
	return ledger, true, nil
}

// withdrawReviewFinding records the orchestrator's withdrawal of a finding assigned
// to an excluded member. The member has no executor left to dispose of it, and its
// card may be final, so the original lives in the ledger alone.
func withdrawReviewFinding(tx *Transaction, run ReviewRun, d ReviewDisposition, expectedRevision uint64) error {
	s, err := tx.Expect(d.TaskID, "", expectedRevision)
	if err != nil {
		return err
	}
	if err = requireWithdrawalBinding(tx, d); err != nil {
		return err
	}
	d.SubmittedRevision = s.Revision
	ledger, fresh, err := appendDisposition(tx, run, &d)
	if err != nil || !fresh {
		return err
	}
	return tx.PutGroup(reviewControlGroup, ledgerName(d.RunID), reviewJSON(ledger))
}

// requireWithdrawalBinding has the disposition kernel judge the three facts of a
// withdrawal in order: the record carries one, the member is excluded, and the
// withdrawal names its exclusion. The probe holds every other fact.
func requireWithdrawalBinding(tx *Transaction, d ReviewDisposition) error {
	allows := func(declared, excluded, named bool) bool {
		_, ok := stepPair(reviewdisposition.State{Assigned: true}, reviewdisposition.Withdraw{Dup: reviewdisposition.DupNone,
			Ref: reviewdisposition.RefEmpty, Expect: true, Declared: declared, Excluded: excluded, Named: named, Ledger: true,
			Available: true, Shape: true, AuthorOk: true, FieldsOk: true, Bound: true, EvidenceOk: true, WaiverOk: true})
		return ok
	}
	if !allows(d.Withdrawal != nil, true, true) {
		return reviewError("withdrawn requires withdrawal.exclusion_id and withdrawal.decision")
	}
	e, excluded, err := readMemberExclusion(tx, d.TaskID)
	if err != nil {
		return err
	}
	if !allows(true, excluded, true) {
		return reviewError("withdrawn is only for an excluded member: " + d.TaskID)
	}
	if !allows(true, true, e.ExclusionID == d.Withdrawal.ExclusionID) {
		return reviewError("withdrawal must name the exclusion of " + d.TaskID + ": " + e.ExclusionID)
	}
	return nil
}

func readDispositionLedger(tx *Transaction, run ReviewRun, complete bool) (dispositionLedger, error) {
	var l dispositionLedger
	ok, err := readReviewJSON(tx, ledgerName(run.RunID), &l)
	if err != nil {
		return l, err
	}
	if !ok {
		if !complete {
			return l, nil
		}
		return l, reviewError("assignment and author dispositions required: " + run.RunID)
	}
	f, err := runFindings(tx, run)
	if err != nil {
		return l, err
	}
	if l.Assignment.RunID != run.RunID || l.Assignment.BatchID != run.BatchID || len(l.Assignment.Items) != len(f.all()) {
		return l, reviewError("assignment coverage/binding")
	}
	targets, err := reviewCopyTargets(tx, run.TaskIDs)
	if err != nil {
		return l, err
	}
	for _, id := range targets {
		text, err := tx.Read(id, "reviews/"+run.RunID+"/assignment.json")
		if err != nil {
			return l, err
		}
		if text != reviewJSON(l.Assignment) {
			return l, reviewError("assignment copy mismatch")
		}
	}
	var mapping LegacyFindingMap
	mapped, err := readReviewJSON(tx, "runs/"+run.RunID+"/legacy-map.json", &mapping)
	if err != nil {
		return l, err
	}
	if mapped {
		for _, id := range targets {
			text, err := tx.Read(id, "reviews/"+run.RunID+"/legacy-map.json")
			if err != nil {
				return l, err
			}
			if text != reviewJSON(mapping) {
				return l, reviewError("legacy mapping copy mismatch")
			}
		}
	}
	items := map[string]ReviewFinding{}
	for _, item := range f.all() {
		items[item.ID] = item
		ids := l.Assignment.Items[item.ID]
		if len(ids) == 0 {
			return l, reviewError("empty finding assignment")
		}
		normalized, e := normalizedReviewTasks(ids)
		if e != nil || !slices.Equal(ids, normalized) {
			return l, reviewError("assignment members")
		}
		for _, id := range ids {
			if !containsID(run.TaskIDs, id) {
				return l, reviewError("foreign assignment")
			}
		}
	}
	latest, seen := map[string]string{}, map[string]bool{}
	for _, d := range l.Records {
		chain := latest[dispositionKey(d)] != ""
		if err = validateDisposition(d, run, items[d.FindingID], l.Assignment); err != nil {
			return l, err
		}
		dup := reviewdisposition.DupNone
		if seen[d.RecordID] {
			dup = reviewdisposition.DupConflict
		}
		event := recordEvent(d, mustFix(items[d.FindingID].Tier), securityReviewRole(run.Role), dup, chainRef(d.PreviousRecordID, latest[dispositionKey(d)]))
		if _, ok := stepPair(pairState(l.Assignment, d.FindingID, d.TaskID, chain), event); !ok {
			return l, reviewError("author record lineage")
		}
		seen[d.RecordID] = true
		latest[dispositionKey(d)] = d.RecordID
		if _, err = time.Parse(time.RFC3339Nano, d.RecordedAt); err != nil {
			return l, reviewError("author timestamp")
		}
		if d.Status == "withdrawn" {
			if err = requireWithdrawalBinding(tx, d); err != nil {
				return l, err
			}
			continue
		}
		excluded, err := memberExcluded(tx, d.TaskID)
		if err != nil {
			return l, err
		}
		if excluded {
			continue
		}
		text, err := tx.Read(d.TaskID, dispositionPath(d))
		if err != nil {
			return l, err
		}
		if text != reviewJSON(d) {
			return l, reviewError("author original mismatch")
		}
	}
	if complete {
		for _, item := range f.all() {
			for _, id := range l.Assignment.Items[item.ID] {
				if latest[item.ID+"/"+id] != "" {
					continue
				}
				excluded, err := memberExcluded(tx, id)
				if err != nil {
					return l, err
				}
				if excluded {
					return l, reviewError(t("board.review_withdrawal_required", run.RunID, item.ID, id))
				}
				return l, reviewError(fmt.Sprintf("missing author disposition: %s/%s/%s", run.RunID, item.ID, id))
			}
		}
	}
	return l, nil
}
func latestDispositions(l dispositionLedger) []ReviewDisposition {
	latest := map[string]ReviewDisposition{}
	for _, d := range l.Records {
		latest[dispositionKey(d)] = d
	}
	out := make([]ReviewDisposition, 0, len(latest))
	for _, d := range latest {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b ReviewDisposition) int { return strings.Compare(dispositionKey(a), dispositionKey(b)) })
	return out
}
