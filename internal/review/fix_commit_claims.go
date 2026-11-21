package review

import (
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// verifyFixCommitClaims runs before the edge checks of a closure. When a rebase
// rewrote a fix commit, the edge checks alone report two SHAs. This check
// reports every affected fixed record at once, so the author can correct each one.
func verifyFixCommitClaims(cwd string, v board.ReviewBatchView, r board.ReviewCloseRequest) error {
	claims, err := board.ReviewFixCommitClaims(v, r)
	if err != nil {
		return err
	}
	var lines []string
	for _, claim := range claims {
		problems, err := fixCommitProblems(cwd, claim)
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			lines = append(lines, "task_id="+claim.TaskID+" run_id="+claim.RunID+" finding_id="+claim.FindingID+" record_id="+claim.RecordID+" fix_commit="+claim.FixCommit+": "+strings.Join(problems, "; "))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	// The literal "Git ancestry" opens the detail in every language, as scripts and tests match on it.
	return archiveError("Git ancestry: " + config.Text("review.fix_commit_history", len(lines)) + "\n  " + strings.Join(lines, "\n  ") + "\n" + config.Text("review.fix_commit_next"))
}

// fixCommitProblems lists why a fixed record's commit breaks the closure edges. A
// commit that does not exist is the only problem listed for it, and a fix commit equal to the
// reviewed commit of its run is listed as such: no fix follows that commit. An edge with
// another missing endpoint is left to the edge check that follows.
func fixCommitProblems(cwd string, claim board.ReviewFixCommitClaim) ([]string, error) {
	out, _, code, err := gitCommand([]string{"cat-file", "-t", claim.FixCommit}, cwd, "")
	if err != nil {
		return nil, err
	}
	if code != 0 || strings.TrimSpace(out) != "commit" {
		return []string{config.Text("review.fix_commit_not_found")}, nil
	}
	var problems []string
	if claim.FixCommit == claim.Reviewed {
		problems = append(problems, config.Text("review.fix_commit_not_later", claim.Reviewed))
	}
	for _, edge := range claim.Edges {
		_, _, code, err = gitCommand([]string{"merge-base", "--is-ancestor", edge.Ancestor, edge.Descendant}, cwd, "")
		if err != nil {
			return nil, err
		}
		if code == 1 {
			problems = append(problems, config.Text("review.fix_commit_not_ancestor", edge.Ancestor, edge.Descendant))
		}
	}
	return problems, nil
}
