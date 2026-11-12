package review

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// These checks apply only to new closures. Historical verification retains the
// original per-assessment diff binding and its unchanged serialized evidence.
func verifyMechanicalClosureScope(ctx context.Context, cwd string, v board.ReviewBatchView, r board.ReviewCloseRequest) error {
	if len(r.Mechanical) == 0 {
		return nil
	}
	declared, documentation := map[string]bool{}, map[string]bool{}
	assessments := map[string][]board.ReviewMechanicalAssessment{}
	for _, a := range r.Mechanical {
		assessments[a.Finding.RunID] = append(assessments[a.Finding.RunID], a)
		for _, name := range a.Paths {
			declared[name] = true
			if a.Category == "documentation" {
				documentation[name] = true
			}
		}
	}
	for _, rv := range v.Runs {
		items := assessments[rv.Run.RunID]
		if len(items) == 0 {
			continue
		}
		conclusion := r.Roles[rv.Run.Role]
		for _, d := range board.ReviewRoleFixedRecords(v, rv.Run.Role) {
			if err := verifyGitEdgeContext(ctx, cwd, board.ReviewGitEdge{Ancestor: d.FixCommit, Descendant: conclusion.PassedAt}); err != nil {
				return archiveError(config.Text("review.mechanical_fix_outside_range", d.RunID, d.FindingID, d.FixCommit, conclusion.PassedAt, conclusion.RunID) + ": " + err.Error())
			}
		}
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", rv.Run.Commit, conclusion.PassedAt}
		out, _, code, err := gitCommandContext(ctx, args, cwd, "")
		if err != nil {
			return err
		}
		if code != 0 {
			return archiveError(config.Text("review.mechanical_diff_failed", rv.Run.RunID))
		}
		var missing []string
		for name := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
			if name != "" && !declared[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			return archiveError(config.Text("review.mechanical_unreported_files", fmt.Sprintf("%q", missing), conclusion.RunID))
		}
		deletionPaths := map[string]bool{}
		for _, a := range items {
			if a.Category != "dead-code" && a.Category != "redundant-test" {
				continue
			}
			for _, name := range a.Paths {
				if !documentation[name] {
					deletionPaths[name] = true
				}
			}
		}
		var additions []string
		for _, name := range slices.Sorted(maps.Keys(deletionPaths)) {
			added, err := mechanicalPathHasAdditions(ctx, cwd, rv.Run.Commit, conclusion.PassedAt, name)
			if err != nil {
				return err
			}
			if added {
				additions = append(additions, name)
			}
		}
		if len(additions) > 0 {
			return archiveError(config.Text("review.mechanical_added_lines", fmt.Sprintf("%q", additions), conclusion.RunID))
		}
	}
	return nil
}

func mechanicalPathHasAdditions(ctx context.Context, cwd, from, to, name string) (bool, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-space-change", "--numstat", "-z", from, to, "--", ":(literal)" + name}
	out, _, code, err := gitCommandContext(ctx, args, cwd, "")
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, archiveError(config.Text("review.mechanical_diff_failed", name))
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		if line == "" {
			continue
		}
		added, _, ok := strings.Cut(line, "\t")
		if !ok {
			return false, archiveError(config.Text("review.mechanical_numstat_invalid", name))
		}
		if added != "0" {
			return true, nil
		}
	}
	return false, nil
}
