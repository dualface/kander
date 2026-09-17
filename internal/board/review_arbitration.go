package board

import (
	"reflect"
	"strings"
	"time"
)

// ReviewArbitration is immutable third-party evidence for a disputed must-fix
// finding. It binds a verdict to the exact latest author disposition that was
// disputed; later author revisions require a new arbitration.
type ReviewArbitration struct {
	Schema              int    `json:"schema"`
	ArbitrationID       string `json:"arbitration_id"`
	RunID               string `json:"run_id"`
	FindingID           string `json:"finding_id"`
	BatchID             string `json:"batch_id"`
	TaskID              string `json:"task_id"`
	DispositionRecordID string `json:"disposition_record_id"`
	Arbiter             string `json:"arbiter"`
	Model               string `json:"model,omitempty"`
	Effort              string `json:"effort,omitempty"`
	Verdict             string `json:"verdict"`
	Basis               string `json:"basis"`
	Report               string `json:"report"`
	ReportHash           string `json:"report_hash"`
	RecordedAt           string `json:"recorded_at"`
}

func arbitrationPath(a ReviewArbitration) string {
	return "reviews/" + a.RunID + "/arbitrations/" + a.ArbitrationID + ".json"
}

func arbitrationName(a ReviewArbitration) string {
	return "runs/" + a.RunID + "/arbitrations/" + a.ArbitrationID + ".json"
}

func validArbitrationVerdict(verdict string) bool {
	switch verdict {
	case "sustain", "overrule", "inconclusive":
		return true
	default:
		return false
	}
}

// SubmitReviewArbitration records a narrow third-party decision about one
// rejected or unverifiable must-fix finding. It does not mutate the author's
// disposition: sustain still requires the author to confirm/fix, overrule may
// support a rejected revision, and inconclusive remains unresolved.
func SubmitReviewArbitration(root string, a ReviewArbitration) error {
	run, err := ReadReviewRun(root, a.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForMutation(tx, a.RunID)
		if err != nil {
			return err
		}
		if a.Schema != 1 || !ValidReviewID(a.ArbitrationID) || a.BatchID != run.BatchID || !containsID(run.TaskIDs, a.TaskID) || strings.TrimSpace(a.FindingID) == "" || strings.TrimSpace(a.DispositionRecordID) == "" {
			return reviewError("arbitration identity")
		}
		if strings.TrimSpace(a.Arbiter) == "" || a.Arbiter == run.Reviewer || strings.TrimSpace(a.Basis) == "" || strings.TrimSpace(a.Report) == "" || !validArbitrationVerdict(a.Verdict) {
			return reviewError("arbitration independence/verdict/evidence")
		}
		if a.ReportHash != ReviewDigest([]byte(a.Report)) {
			return reviewError("arbitration report hash")
		}

		findings, err := runFindings(tx, run)
		if err != nil {
			return err
		}
		var item ReviewFinding
		found := false
		for _, f := range findings.all() {
			if f.ID == a.FindingID {
				item = f
				found = true
				break
			}
		}
		if !found || !mustFix(item.Tier) {
			return reviewError("arbitration requires must-fix finding")
		}

		ledger, err := readDispositionLedger(tx, run, true)
		if err != nil {
			return err
		}
		if !containsID(ledger.Assignment.Items[a.FindingID], a.TaskID) {
			return reviewError("arbitration task assignment")
		}
		latest := latestDispositions(ledger)
		var disposition ReviewDisposition
		found = false
		for _, d := range latest {
			if dispositionKey(d) == a.FindingID+"/"+a.TaskID {
				disposition = d
				found = true
				break
			}
		}
		if !found || disposition.RecordID != a.DispositionRecordID {
			return reviewError("arbitration must bind latest disposition")
		}
		if disposition.Status != "rejected" && disposition.Status != "unverifiable" {
			return reviewError("arbitration requires disputed disposition")
		}
		if a.Arbiter == disposition.Author {
			return reviewError("arbiter must differ from author")
		}

		var existing ReviewArbitration
		exists, err := readReviewJSON(tx, arbitrationName(a), &existing)
		if err != nil {
			return err
		}
		if exists {
			a.RecordedAt = existing.RecordedAt
			if reflect.DeepEqual(a, existing) {
				return nil
			}
			return reviewError("immutable arbitration conflict")
		}
		a.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err = tx.Put(a.TaskID, arbitrationPath(a), reviewJSON(a)); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, arbitrationName(a), reviewJSON(a))
	})
}
