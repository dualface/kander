package review

import (
	"strings"

	"github.com/dualface/kander/internal/board"
)

func parseReviewRole(roleInput string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(roleInput)) {
	case "pmqa":
		return "PMQA", true
	case "security":
		return "Security", true
	case "pm":
		return "PM", true
	case "qa":
		return "QA", true
	case "csa", "codesecurityanalyst":
		return "CSA", true
	case "hacker":
		return "Hacker", true
	default:
		return "", false
	}
}

func currentReviewRole(role string) bool {
	return role == "PMQA" || role == "Security"
}

func reviewerConfigRole(role string) string {
	switch role {
	case "PM", "QA":
		return "PMQA"
	case "CSA", "Hacker":
		return "Security"
	default:
		return role
	}
}

func admitReviewRole(role string, root string, options archiveOptions, replay bool, existing board.ReviewRun) bool {
	if currentReviewRole(role) {
		return true
	}
	if replay && existing.Role == role {
		return true
	}
	if root == "" || options.batchID == "" {
		return false
	}
	batch, err := board.ReadReviewBatch(root, options.batchID)
	if err != nil {
		return false
	}
	return batch.Requirements[role] == "required"
}
