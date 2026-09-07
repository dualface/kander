package board

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
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
func runFindings(tx *Transaction, run ReviewRun) (findings ReviewFindings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", run.RunID, err)
		}
	}()
	data, ok, err := tx.ReadGroup(reviewControlGroup, "runs/"+run.RunID+"/originals/report.md")
	if err != nil {
		return ReviewFindings{}, err
	}
	if !ok || ReviewDigest(data) != run.Hashes["report.md"] {
		return ReviewFindings{}, reviewError("missing report original")
	}
	f, parseErr := ParseReviewFindings(data)
	if parseErr == nil {
		return f, nil
	}
	if run.FindingsSchema > 0 {
		return f, parseErr
	}
	var mapping LegacyFindingMap
	ok, err = readReviewJSON(tx, "runs/"+run.RunID+"/legacy-map.json", &mapping)
	if err != nil {
		return f, err
	}
	if !ok {
		return f, parseErr
	}
	if mapping.RunID != run.RunID {
		return f, reviewError("legacy map run")
	}
	if err = validateLegacyMap(mapping, data); err != nil {
		return f, err
	}
	return mapping.Findings, nil
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

func reviewRunForMutation(tx *Transaction, runID string) (run ReviewRun, err error) {
	run, err = reviewRunForConsumption(tx, runID)
	if err != nil {
		return
	}
	var closed ReviewClosure
	ok, err := readReviewJSON(tx, closureName(run.BatchID), &closed)
	if err == nil && ok {
		err = reviewError("batch already closed")
	}
	return
}
func MapLegacyReview(root string, m LegacyFindingMap) error {
	run, err := ReadReviewRun(root, m.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, m.RunID)
		if err != nil {
			return err
		}
		if run.FindingsSchema > 0 {
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
			if reflect.DeepEqual(old, m) {
				return nil
			}
			return reviewError("immutable legacy map")
		}
		m.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for _, id := range run.TaskIDs {
			if err = tx.Put(id, "reviews/"+m.RunID+"/legacy-map.json", reviewJSON(m)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, name, reviewJSON(m))
	})
}
func AssignReviewFindings(root string, a ReviewAssignment) error {
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
		f, err := runFindings(tx, run)
		if err != nil {
			return err
		}
		if err = validateFindingLineage(tx, run, f); err != nil {
			return err
		}
		if len(a.Items) != len(f.all()) {
			return reviewError("assignment must cover exactly every finding")
		}
		a.Owners = map[string]string{}
		for _, item := range f.all() {
			ids, err := normalizedReviewTasks(a.Items[item.ID])
			if err != nil {
				return err
			}
			a.Items[item.ID] = ids
			for _, id := range ids {
				if !containsID(run.TaskIDs, id) {
					return reviewError("foreign assignment")
				}
				s, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				owner := MetadataFrom(s.Text, FieldOwner)
				if strings.TrimSpace(owner) == "" {
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
			if reflect.DeepEqual(a, old.Assignment) {
				return nil
			}
			return reviewError("immutable assignment conflict")
		}
		a.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for _, id := range run.TaskIDs {
			if err = tx.Put(id, "reviews/"+run.RunID+"/assignment.json", reviewJSON(a)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(dispositionLedger{Assignment: a, Records: []ReviewDisposition{}}))
	})
}
func validateDisposition(d ReviewDisposition, run ReviewRun, item ReviewFinding, a ReviewAssignment) error {
	if !ValidReviewID(d.RecordID) || d.RunID != run.RunID || d.BatchID != run.BatchID || d.FindingID != item.ID || !containsID(a.Items[item.ID], d.TaskID) || d.Author != a.Owners[d.TaskID] && d.SubmittedRevision == 0 || d.Author == "" {
		return reviewError("disposition identity/ownership")
	}
	if d.ReportHash != run.Hashes["report.md"] || d.Original != item.Text || strings.TrimSpace(d.Basis) == "" {
		return reviewError("disposition original and basis required")
	}
	if d.Status != "confirmed" && d.Status != "fixed" && d.Status != "rejected" && d.Status != "unverifiable" && d.Status != "waived" && d.Status != "deferred" {
		return reviewError("disposition status")
	}
	if !mustFix(item.Tier) && d.Status != "fixed" && d.Status != "deferred" && d.Status != "rejected" {
		return reviewError("non-blocking status")
	}
	if mustFix(item.Tier) && d.Status == "deferred" {
		return reviewError("must-fix cannot be deferred")
	}
	if d.Status == "fixed" && (!validCommit(d.FixCommit) || strings.TrimSpace(d.Verification) == "") {
		return reviewError("fixed requires commit and verification")
	}
	if d.Status != "fixed" && (d.FixCommit != "" || d.Mechanical != "") {
		return reviewError("fix evidence without fixed status")
	}
	if d.Mechanical != "" && !mechanicalCategory(d.Mechanical) {
		return reviewError("mechanical category")
	}
	if d.Status == "waived" {
		if run.Role != "CSA" && run.Role != "Hacker" || d.Waiver == nil || strings.TrimSpace(d.Waiver.Decision) == "" {
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
func SubmitReviewDisposition(root string, d ReviewDisposition, expectedRevision uint64) error {
	run, err := ReadReviewRun(root, d.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, d.RunID)
		if err != nil {
			return err
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
		var ledger dispositionLedger
		exists, err := readReviewJSON(tx, ledgerName(d.RunID), &ledger)
		if err != nil {
			return err
		}
		if !exists {
			return reviewError("assignment required")
		}
		findings, err := runFindings(tx, run)
		if err != nil {
			return err
		}
		var item ReviewFinding
		for _, f := range findings.all() {
			if f.ID == d.FindingID {
				item = f
			}
		}
		if err = validateDisposition(d, run, item, ledger.Assignment); err != nil {
			return err
		}
		previous := ""
		for _, record := range ledger.Records {
			if record.RecordID == d.RecordID {
				d.RecordedAt = record.RecordedAt
				d.SubmittedRevision = record.SubmittedRevision
				if reflect.DeepEqual(d, record) {
					return nil
				}
				return reviewError("immutable author record")
			}
			if dispositionKey(record) == dispositionKey(d) {
				previous = record.RecordID
			}
		}
		if d.PreviousRecordID != previous {
			return reviewError("disposition predecessor CAS conflict")
		}
		d.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		ledger.Records = append(ledger.Records, d)
		if err = tx.Put(d.TaskID, dispositionPath(d), reviewJSON(d)); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, ledgerName(d.RunID), reviewJSON(ledger))
	})
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
	for _, id := range run.TaskIDs {
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
		for _, id := range run.TaskIDs {
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
		if err = validateDisposition(d, run, items[d.FindingID], l.Assignment); err != nil {
			return l, err
		}
		if seen[d.RecordID] || latest[dispositionKey(d)] != d.PreviousRecordID {
			return l, reviewError("author record lineage")
		}
		seen[d.RecordID] = true
		latest[dispositionKey(d)] = d.RecordID
		if _, err = time.Parse(time.RFC3339Nano, d.RecordedAt); err != nil {
			return l, reviewError("author timestamp")
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
				if latest[item.ID+"/"+id] == "" {
					return l, reviewError(fmt.Sprintf("missing author disposition: %s/%s/%s", run.RunID, item.ID, id))
				}
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
