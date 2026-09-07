package board

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// FindingRef identifies an item without relying on wall-clock order or prose mentions.
type FindingRef struct {
	RunID     string `json:"run_id"`
	FindingID string `json:"finding_id"`
}
type ReviewFinding struct {
	ID         string      `json:"id"`
	Tier       string      `json:"tier"`
	Text       string      `json:"text"`
	Evidence   string      `json:"evidence"`
	Mechanical string      `json:"mechanical,omitempty"`
	Lineage    *FindingRef `json:"lineage,omitempty"`
}
type ReviewFindings struct {
	Findings    []ReviewFinding `json:"FINDINGS"`
	NonBlocking []ReviewFinding `json:"NON_BLOCKING"`
}

var findingIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var commitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func validCommit(s string) bool  { return commitPattern.MatchString(s) }
func (r FindingRef) key() string { return r.RunID + "/" + r.FindingID }
func (f ReviewFindings) all() []ReviewFinding {
	return append(append([]ReviewFinding{}, f.Findings...), f.NonBlocking...)
}
func mustFix(tier string) bool { return tier == "blocking" || tier == "high" || tier == "medium" }
func mechanicalCategory(s string) bool {
	return s == "documentation" || s == "dead-code" || s == "redundant-test"
}

// DecodeReviewJSON rejects unknown fields and trailing values at controlled entrances.
func DecodeReviewJSON(data []byte, value any) error {
	tokens := json.NewDecoder(strings.NewReader(string(data)))
	if err := uniqueReviewJSON(tokens, 0); err != nil {
		return reviewError(err.Error())
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return reviewError(err.Error())
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return reviewError("trailing JSON")
	}
	return nil
}
func validateFindings(f ReviewFindings) error {
	if f.Findings == nil || f.NonBlocking == nil {
		return reviewError("FINDINGS and NON_BLOCKING arrays required")
	}
	seen := map[string]bool{}
	for section, items := range [][]ReviewFinding{f.Findings, f.NonBlocking} {
		for _, item := range items {
			if !findingIDPattern.MatchString(item.ID) || seen[item.ID] || strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.Evidence) == "" {
				return reviewError("missing/duplicate finding ID, text or evidence")
			}
			seen[item.ID] = true
			if section == 0 && !mustFix(item.Tier) || section == 1 && item.Tier != "low" && item.Tier != "recommend" && item.Tier != "suggest" {
				return reviewError("finding tier/section mismatch")
			}
			if item.Mechanical != "" && (!mustFix(item.Tier) || !mechanicalCategory(item.Mechanical)) {
				return reviewError("mechanical category")
			}
			if item.Lineage != nil && (!ValidReviewID(item.Lineage.RunID) || !findingIDPattern.MatchString(item.Lineage.FindingID)) {
				return reviewError("finding lineage")
			}
		}
	}
	return nil
}

// ParseReviewFindings reads exactly one dedicated block. IDs elsewhere are data,
// never inferred findings. Legacy prose requires a separate, explicit mapping.
func ParseReviewFindings(report []byte) (ReviewFindings, error) {
	var f ReviewFindings
	lines := strings.Split(strings.ReplaceAll(string(report), "\r\n", "\n"), "\n")
	start, end := -1, -1
	for i, line := range lines {
		if line == "```kander-findings" {
			if start >= 0 {
				return f, reviewError("duplicate findings block")
			}
			start = i
		}
		if start >= 0 && end < 0 && i > start && line == "```" {
			end = i
		}
	}
	if start < 0 || end < 0 {
		return f, reviewError("structured findings required; legacy mapping required for old reports")
	}
	if err := DecodeReviewJSON([]byte(strings.Join(lines[start+1:end], "\n")), &f); err != nil {
		return f, err
	}
	return f, validateFindings(f)
}

type LegacyFindingLocation struct {
	FindingID string `json:"finding_id"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Quote     string `json:"quote"`
}

// LegacyFindingMap preserves the report and records a human's complete mapping.
// The attestation is accountable provenance, not a natural-language completeness proof.
type LegacyFindingMap struct {
	RunID      string                  `json:"run_id"`
	ReportHash string                  `json:"report_hash"`
	Author     string                  `json:"author"`
	Basis      string                  `json:"basis"`
	Complete   bool                    `json:"complete"`
	Findings   ReviewFindings          `json:"findings"`
	Locations  []LegacyFindingLocation `json:"locations"`
	RecordedAt string                  `json:"recorded_at"`
}

func validateLegacyMap(m LegacyFindingMap, report []byte) error {
	if m.ReportHash != ReviewDigest(report) || !m.Complete || strings.TrimSpace(m.Author) == "" || strings.TrimSpace(m.Basis) == "" {
		return reviewError("legacy mapping provenance/completeness")
	}
	if strings.Contains(string(report), "```kander-findings") {
		return reviewError("new structured reports cannot use legacy mapping")
	}
	if err := validateFindings(m.Findings); err != nil {
		return err
	}
	if len(m.Findings.all()) == 0 || len(m.Locations) != len(m.Findings.all()) {
		return reviewError("legacy mapping cannot be empty")
	}
	lines := strings.Split(strings.ReplaceAll(string(report), "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	for _, l := range m.Locations {
		if seen[l.FindingID] || l.StartLine < 1 || l.EndLine < l.StartLine || l.EndLine > len(lines) || l.Quote != strings.Join(lines[l.StartLine-1:l.EndLine], "\n") || strings.TrimSpace(l.Quote) == "" {
			return reviewError("legacy original location")
		}
		seen[l.FindingID] = true
	}
	for _, f := range m.Findings.all() {
		if !seen[f.ID] {
			return reviewError(fmt.Sprintf("legacy mapping location: %s", f.ID))
		}
	}
	return nil
}

// Duplicate object keys could otherwise replace a non-empty finding array with
// an empty one. Reject them before decoding any review-controlled JSON input.
func uniqueReviewJSON(d *json.Decoder, depth int) error {
	if depth > 128 {
		return fmt.Errorf("JSON nesting limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate JSON key: %v", key)
			}
			seen[name] = true
			if err = uniqueReviewJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = uniqueReviewJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}
