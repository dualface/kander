package check

import "github.com/dualface/kander/internal/config"

const (
	schemaVersion = 1

	checkDelivery = "delivery"
	checkOverlap  = "overlap"

	statusPass           = "pass"
	statusFail           = "fail"
	statusReviewRequired = "review-required"
	statusActionRequired = "action-required"
	statusError          = "error"

	candidateStatus = "review-required"

	lineLimit = 1000

	exitOK     = 0
	exitAction = 1
	exitUsage  = 2
	exitExec   = 3

	errCodeUsage          = "usage"
	errCodeInvalidRef     = "invalid-ref"
	errCodeNotAncestor    = "not-ancestor"
	errCodeNotRepository  = "not-repository"
	errCodeGitUnavailable = "git-unavailable"
	errCodeNoMergeBase    = "no-merge-base"
	errCodeOutputLimit    = "output-limit"
	errCodeInternal       = "internal"
	errCodeParse          = "parse"
)

// CheckError is the stable error object embedded in JSON results.
type CheckError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DiffCheck is the git diff --check section of a delivery result.
type DiffCheck struct {
	Status      string   `json:"status"`
	Diagnostics []string `json:"diagnostics"`
}

// LineCandidate is one over-limit added or crossed file.
type LineCandidate struct {
	Status      string   `json:"status"`
	Path        GitPath  `json:"path"`
	BasePath    *GitPath `json:"base_path"`
	BaseLines   int      `json:"base_lines"`
	TargetLines int      `json:"target_lines"`
}

// DeliveryResult is the frozen delivery JSON object.
type DeliveryResult struct {
	SchemaVersion  int             `json:"schema_version"`
	Check          string          `json:"check"`
	Status         string          `json:"status"`
	BaseCommit     string          `json:"base_commit"`
	TargetCommit   string          `json:"target_commit"`
	DiffCheck      DiffCheck       `json:"diff_check"`
	AddedOverLimit []LineCandidate `json:"added_over_limit"`
	CrossedLimit   []LineCandidate `json:"crossed_limit"`
	Error          *CheckError     `json:"error"`
}

// OverlapResult is the frozen overlap JSON object.
type OverlapResult struct {
	SchemaVersion int         `json:"schema_version"`
	Check         string      `json:"check"`
	Status        string      `json:"status"`
	MergeBase     string      `json:"merge_base"`
	HeadCommit    string      `json:"head_commit"`
	SourceCommit  string      `json:"source_commit"`
	Paths         []GitPath   `json:"paths"`
	Error         *CheckError `json:"error"`
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func emptyCandidates() []LineCandidate {
	return make([]LineCandidate, 0)
}

func emptyDiagnostics() []string {
	return make([]string, 0)
}

func emptyPaths() []GitPath {
	return make([]GitPath, 0)
}

func deliveryError(code, message string) DeliveryResult {
	return DeliveryResult{
		SchemaVersion:  schemaVersion,
		Check:          checkDelivery,
		Status:         statusError,
		DiffCheck:      DiffCheck{Status: statusPass, Diagnostics: emptyDiagnostics()},
		AddedOverLimit: emptyCandidates(),
		CrossedLimit:   emptyCandidates(),
		Error:          &CheckError{Code: code, Message: message},
	}
}

func overlapError(code, message string) OverlapResult {
	return OverlapResult{
		SchemaVersion: schemaVersion,
		Check:         checkOverlap,
		Status:        statusError,
		Paths:         emptyPaths(),
		Error:         &CheckError{Code: code, Message: message},
	}
}

func deliveryStatus(diffFail, hasCandidates bool) string {
	switch {
	case diffFail:
		return statusFail
	case hasCandidates:
		return statusReviewRequired
	default:
		return statusPass
	}
}

func overlapStatus(hasPaths bool) string {
	if hasPaths {
		return statusActionRequired
	}
	return statusPass
}

func completedExit(status string) int {
	switch status {
	case statusPass:
		return exitOK
	case statusFail, statusReviewRequired, statusActionRequired:
		return exitAction
	default:
		return exitExec
	}
}
