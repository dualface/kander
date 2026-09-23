package board

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

// FindingsSchemaReceiver keeps report formatting separate from execution facts.
// Older runs retain their original validation and closure representation.
const FindingsSchemaReceiver = 2

var errInterpretationPending = errors.New("review interpretation pending")

// ReviewReportQuote binds an interpretation to exact, one-based report lines.
type ReviewReportQuote struct {
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Quote     string `json:"quote"`
}

// ReviewInterpretation is an accountable reading of the complete report, not
// a proof that natural-language findings were exhaustively identified.
type ReviewInterpretation struct {
	RunID      string                  `json:"run_id"`
	ReportHash string                  `json:"report_hash"`
	Author     string                  `json:"author"`
	Basis      string                  `json:"basis"`
	Complete   bool                    `json:"complete"`
	Findings   ReviewFindings          `json:"findings"`
	Locations  []LegacyFindingLocation `json:"locations"`
	NoFindings *ReviewReportQuote      `json:"no_findings,omitempty"`
	RecordedAt string                  `json:"recorded_at,omitempty"`
}

func interpretationName(runID string) string { return "runs/" + runID + "/interpretation.json" }
func interpretationPath(runID string) string { return "reviews/" + runID + "/interpretation.json" }

func validateInterpretation(m ReviewInterpretation, run ReviewRun, report []byte) error {
	if run.FindingsSchema != FindingsSchemaReceiver || m.RunID != run.RunID || m.ReportHash != ReviewDigest(report) || !m.Complete || strings.TrimSpace(m.Author) == "" || strings.TrimSpace(m.Basis) == "" {
		return reviewError("interpretation provenance/completeness")
	}
	if strings.TrimSpace(string(report)) == "" {
		return reviewError("interpretation requires a nonempty original report")
	}
	if err := validateFindings(m.Findings); err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(report), "\r\n", "\n"), "\n")
	validQuote := func(start, end int, quote string) bool {
		return start >= 1 && end >= start && end <= len(lines) && strings.TrimSpace(quote) != "" && quote == strings.Join(lines[start-1:end], "\n")
	}
	items := m.Findings.all()
	if len(items) == 0 {
		if len(m.Locations) != 0 || m.NoFindings == nil || !validQuote(m.NoFindings.StartLine, m.NoFindings.EndLine, m.NoFindings.Quote) {
			return reviewError("zero findings require an explicit original no-findings statement and basis")
		}
		return nil
	}
	if m.NoFindings != nil || len(m.Locations) != len(items) {
		return reviewError("interpretation must cite every finding and cannot assert zero findings")
	}
	seen := map[string]bool{}
	for _, location := range m.Locations {
		if seen[location.FindingID] || !validQuote(location.StartLine, location.EndLine, location.Quote) {
			return reviewError("interpretation original location")
		}
		seen[location.FindingID] = true
	}
	for _, item := range items {
		if !seen[item.ID] {
			return reviewError("interpretation missing finding location: " + item.ID)
		}
	}
	return nil
}

// readInterpretation verifies all copies, including orphaned card copies when
// the control record is absent. Missing evidence must not become a new mapping.
func readInterpretation(tx *Transaction, run ReviewRun, report []byte) (ReviewInterpretation, bool, error) {
	var m ReviewInterpretation
	exists, err := readReviewJSON(tx, interpretationName(run.RunID), &m)
	if err != nil {
		return m, exists, err
	}
	if exists {
		if err = validateInterpretation(m, run, report); err != nil {
			return m, true, err
		}
		if _, err = time.Parse(time.RFC3339Nano, m.RecordedAt); err != nil {
			return m, true, reviewError("interpretation timestamp")
		}
	}
	for _, id := range run.TaskIDs {
		text, err := tx.Read(id, interpretationPath(run.RunID))
		if !exists && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return m, exists, err
		}
		if !exists || text != reviewJSON(m) {
			return m, exists, reviewError("interpretation copy mismatch")
		}
	}
	return m, exists, nil
}

func receiverFindings(tx *Transaction, run ReviewRun, report []byte) (ReviewFindings, error) {
	m, exists, err := readInterpretation(tx, run, report)
	if err != nil {
		return ReviewFindings{}, err
	}
	if exists {
		return m.Findings, nil
	}
	f, err := ParseReviewFindings(report)
	if err != nil {
		return f, fmt.Errorf("%w: %w", errInterpretationPending, reviewError("report needs review interpret: "+err.Error()))
	}
	return f, nil
}

// InterpretReview publishes one immutable interpretation atomically to every
// member. It never changes the original run, report, manifest, or execution exit.
func InterpretReview(root string, m ReviewInterpretation) error {
	run, err := ReadReviewRun(root, m.RunID)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		run, err := reviewRunForConsumption(tx, m.RunID)
		if err != nil {
			return err
		}
		report, ok, err := tx.ReadGroup(reviewControlGroup, "runs/"+run.RunID+"/originals/report.md")
		if err != nil {
			return err
		}
		if !ok || ReviewDigest(report) != run.Hashes["report.md"] {
			return reviewError("missing report original")
		}
		if err = validateInterpretation(m, run, report); err != nil {
			return err
		}
		old, exists, err := readInterpretation(tx, run, report)
		if err != nil {
			return err
		}
		if exists {
			m.RecordedAt = old.RecordedAt
			if !reflect.DeepEqual(old, m) {
				return reviewError("immutable interpretation conflict")
			}
			return validateFindingLineage(tx, run, m.Findings)
		}
		if _, err = reviewRunForMutation(tx, m.RunID); err != nil {
			return err
		}
		if _, err = ParseReviewFindings(report); err == nil {
			return reviewError("structured findings already available; interpretation cannot replace them")
		}
		if err = validateFindingLineage(tx, run, m.Findings); err != nil {
			return err
		}
		m.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for _, id := range run.TaskIDs {
			if err = tx.Put(id, interpretationPath(run.RunID), reviewJSON(m)); err != nil {
				return err
			}
		}
		return tx.PutGroup(reviewControlGroup, interpretationName(run.RunID), reviewJSON(m))
	})
}

// ReviewNeedsInterpretation reports a normal pending state, while archive damage
// remains an error. Callers can show an actionable receipt after publication.
func ReviewNeedsInterpretation(root, runID string) (pending bool, err error) {
	run, err := ReadReviewRun(root, runID)
	if err != nil || run.FindingsSchema != FindingsSchemaReceiver || run.ExecutionStatus != "ok" {
		return false, err
	}
	err = WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error {
		if err := verifyPublishedReview(tx, run); err != nil {
			return err
		}
		_, err := runFindings(tx, run)
		if errors.Is(err, errInterpretationPending) {
			pending = true
			return nil
		}
		return err
	})
	return
}
