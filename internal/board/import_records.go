package board

import (
	"encoding/hex"
	"slices"
	"strings"
)

// ImportRecordKey identifies one indivisible record by its producer and ID.
// Multiple files may implement the same record.
type ImportRecordKey struct {
	Kind string
	ID   string
}

// ImportCardRecordReference identifies the shared record a card-local copy
// belongs to. The copy still moves as part of its card, never on its own.
func ImportCardRecordReference(rel string) ImportRecordKey {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return ImportRecordKey{}
	}
	switch {
	case parts[0] == "dispatches" && validDispatchID(parts[1]):
		return ImportRecordKey{"dispatch", parts[1]}
	case parts[0] == "reviews" && parts[1] == "batches" && len(parts) > 3 && ValidReviewID(parts[2]):
		return ImportRecordKey{"review-batch", parts[2]}
	case parts[0] == "reviews" && ValidReviewID(parts[1]):
		return ImportRecordKey{"review-run", parts[1]}
	}
	return ImportRecordKey{}
}

// ImportRecord identifies a regular, slash-relative file below .kander.
// Runtime files have kind "runtime". Unknown files (including v2-only
// producers) return the zero key and must not be copied from a v1 input.
func ImportRecord(rel string) ImportRecordKey {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != ".kander" {
		return ImportRecordKey{}
	}
	path := parts[2:]
	switch parts[1] {
	case "locks", "caches":
		return ImportRecordKey{"runtime", parts[1]}
	case "versions":
		if id, ok := importJSONID(path, taskIDRe.MatchString); ok {
			return ImportRecordKey{"version", id}
		}
	case "operations":
		// Legacy flat history is partitioned by RecoverTransactions. Recognize
		// it on the target as the same ID, never as a second operation.
		if len(path) == 2 && path[0] == "committed" {
			path = path[1:]
		}
		if len(path) == 1 && journalName.MatchString(path[0]) {
			return ImportRecordKey{"operation", strings.ToLower(strings.TrimSuffix(path[0], ".json"))}
		}
	case "groups":
		return importGroupRecord(path)
	case "issue-results":
		if len(path) == 3 && path[0] == "v1" && importHexID(path[1]) {
			switch path[2] {
			case "state.json":
				return ImportRecordKey{"issue-result", path[1]}
			case "lock":
				return ImportRecordKey{"runtime", "issue-result-lock"}
			}
		}
	}
	return ImportRecordKey{}
}

func importGroupRecord(path []string) ImportRecordKey {
	if len(path) < 2 {
		return ImportRecordKey{}
	}
	group, path := path[0], path[1:]
	switch group {
	case dispatchRegistry:
		if id, ok := importJSONID(path, validDispatchID); ok {
			return ImportRecordKey{"dispatch", id}
		}
	case taskStartGroup:
		switch {
		case len(path) == 2 && path[1] == "current.json":
		case len(path) == 3 && path[1] == "transitions" && importJSONFile(path[2]):
		case len(path) == 3 && validDispatchID(path[1]) &&
			slices.Contains([]string{"pending.json", "result.json", "unresolved.json", "handed-off.json", "process.json"}, path[2]):
		default:
			return ImportRecordKey{}
		}
		if taskIDRe.MatchString(path[0]) {
			return ImportRecordKey{"execution", path[0]}
		}
	case pruneControlGroup:
		if len(path) == 2 && (path[0] == "pending" || path[0] == "receipts") {
			if id, ok := importJSONID(path[1:], taskIDRe.MatchString); ok {
				return ImportRecordKey{"prune", id}
			}
		}
	case reviewControlGroup:
		return importReviewRecord(path)
	default:
		if strings.HasPrefix(group, "00000000-") || !taskGroupRe.MatchString(group) {
			break
		}
		switch {
		case len(path) == 1 && path[0] == "checkpoint.json":
			return ImportRecordKey{"group", group}
		case len(path) == 2 && slices.Contains([]string{"checkpoints", "recoveries", "removals", "wrap-up-git"}, path[0]) && importJSONFile(path[1]):
			return ImportRecordKey{"group", group}
		}
	}
	return ImportRecordKey{}
}

func importReviewRecord(path []string) ImportRecordKey {
	switch path[0] {
	case "runs":
		if len(path) < 3 || !ValidReviewID(path[1]) {
			break
		}
		switch {
		case len(path) == 3 && slices.Contains([]string{"run.json", "sidecar.json", "dispositions.json", "interpretation.json"}, path[2]):
			return ImportRecordKey{"review-run", path[1]}
		case len(path) == 4 && importReviewArtifact(path[2], path[3]):
			return ImportRecordKey{"review-run", path[1]}
		}
	case "batches", "closures", "dispositions":
		if id, ok := importJSONID(path[1:], ValidReviewID); ok {
			return ImportRecordKey{"review-batch", id}
		}
	case "plans":
		if id, ok := importJSONID(path[1:], ValidReviewID); ok {
			return ImportRecordKey{"review-plan", id}
		}
	case "plan-history":
		if len(path) == 3 && ValidReviewID(path[1]) && importJSONFile(path[2]) {
			return ImportRecordKey{"review-plan", path[1]}
		}
	case "task-plans", "tracked-cycles", "exclusions", "recovery-reviews":
		if id, ok := importJSONID(path[1:], taskIDRe.MatchString); ok {
			return ImportRecordKey{"review-task", id}
		}
	}
	return ImportRecordKey{}
}

func importReviewArtifact(dir, name string) bool {
	switch name {
	case "task-context.md", "review-context.md", "prompt.txt", "review-contract.md", "evidence.txt":
		return dir == "inputs" || dir == "originals"
	case "output.raw", "stdout.log", "error.log", ReviewProcessEvidence:
		return dir == "staging" || dir == "originals"
	case "report.md":
		return dir == "originals"
	}
	return false
}

func importJSONID(path []string, valid func(string) bool) (string, bool) {
	if len(path) != 1 {
		return "", false
	}
	id, ok := strings.CutSuffix(path[0], ".json")
	return id, ok && valid(id)
}

func importJSONFile(name string) bool { return strings.HasSuffix(name, ".json") }

func importHexID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 64 && id == strings.ToLower(id) && err == nil
}
