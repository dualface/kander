package board

import (
	"path"
	"reflect"
	"slices"
	"strings"
)

// ReviewMechanicalAssessment is the closing main agent's independent judgment,
// separate from both the reviewer's label and the executing author's record.
// Facts record the category-specific verification; Git binds its exact scope.
type ReviewMechanicalAssessment struct {
	RecordID         string     `json:"record_id"`
	Finding          FindingRef `json:"finding"`
	TaskID           string     `json:"task_id"`
	Author           string     `json:"author"`
	Category         string     `json:"category"`
	ReportedCategory string     `json:"reported_category"`
	ReportHash       string     `json:"report_hash"`
	FixCommit        string     `json:"fix_commit"`
	Basis            string     `json:"basis"`
	Facts            string     `json:"facts"`
	Paths            []string   `json:"paths"`
	DiffHash         string     `json:"diff_hash"`
}

type ReviewMechanicalGit struct {
	RecordID string   `json:"record_id"`
	RunID    string   `json:"run_id"`
	TaskID   string   `json:"task_id"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Paths    []string `json:"paths"`
	DiffHash string   `json:"diff_hash"`
}

func assessmentKey(run, record, task string) string { return run + "/" + record + "/" + task }

// ReviewMechanicalClaims rejects a bare mechanical label. It requires a main
// agent's separately attributed, category-specific assessment of each exception.
// It does not infer semantic correctness from a reviewer label or a Git hash.
func ReviewMechanicalClaims(v ReviewBatchView, r ReviewCloseRequest) ([]ReviewMechanicalGit, error) {
	assessments := map[string]ReviewMechanicalAssessment{}
	for _, a := range r.Mechanical {
		key := assessmentKey(a.Finding.RunID, a.RecordID, a.TaskID)
		if _, exists := assessments[key]; exists {
			return nil, reviewError("duplicate mechanical assessment")
		}
		assessments[key] = a
	}
	var claims []ReviewMechanicalGit
	for _, rv := range v.Runs {
		if r.Roles[rv.Run.Role].RunID != rv.Run.RunID || rv.Findings == nil {
			continue
		}
		items := map[string]ReviewFinding{}
		for _, f := range rv.Findings.all() {
			items[f.ID] = f
		}
		for _, d := range latestViewRecords(rv) {
			item := items[d.FindingID]
			if d.Status != "fixed" || !mustFix(item.Tier) || d.Mechanical == "" {
				continue
			}
			key := assessmentKey(d.RunID, d.RecordID, d.TaskID)
			a, ok := assessments[key]
			if !ok || a.Author != r.Author || strings.TrimSpace(a.Author) == "" || a.Finding.FindingID != item.ID || a.Category != d.Mechanical || a.ReportedCategory != item.Mechanical || a.ReportHash != d.ReportHash || a.FixCommit != d.FixCommit || strings.TrimSpace(a.Basis) == "" || strings.TrimSpace(a.Facts) == "" {
				return nil, reviewError("mechanical exception requires independent main-agent classification and verification: " + key)
			}
			if len(a.DiffHash) != 64 || strings.Trim(a.DiffHash, "0123456789abcdef") != "" || len(a.Paths) == 0 || !slices.IsSorted(a.Paths) || len(slices.Compact(slices.Clone(a.Paths))) != len(a.Paths) {
				return nil, reviewError("mechanical diff hash and sorted unique scope required")
			}
			for _, name := range a.Paths {
				if name == "." || name == ".." || name == "" || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") {
					return nil, reviewError("mechanical scope must use repository-relative paths")
				}
			}
			claims = append(claims, ReviewMechanicalGit{d.RecordID, d.RunID, d.TaskID, rv.Run.Commit, d.FixCommit, a.Paths, a.DiffHash})
			delete(assessments, key)
		}
	}
	if len(assessments) > 0 {
		return nil, reviewError("unreferenced mechanical assessment")
	}
	return claims, nil
}

func verifyMechanicalEvidence(v ReviewBatchView, r ReviewCloseRequest, evidence ReviewGitEvidence) error {
	claims, err := ReviewMechanicalClaims(v, r)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(claims, evidence.Mechanical) {
		return reviewError("mechanical Git evidence binding")
	}
	return nil
}
