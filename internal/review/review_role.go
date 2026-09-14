package review

import "strings"

func canonicalizeReviewRole(roleInput string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(roleInput)) {
	case "pmqa":
		return "PMQA", true
	case "security":
		return "Security", true
	default:
		return "", false
	}
}
