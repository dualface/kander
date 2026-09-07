package launch

import (
	"context"
	"os/exec"
	"regexp"

	"github.com/dualface/kander/internal/board"
)

var integrationHunk = regexp.MustCompile(`(?m)^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@`)
var integrationIndex = regexp.MustCompile(`(?m)^index [0-9a-f]+\.\.[0-9a-f]+(?: [0-7]+)?\n`)

// Compare complete patches, preserving whitespace, context, modes and binary
// changes. Only blob hashes and line offsets may vary after an unrelated rebase.
// Unlike patch-id, this does not ignore meaningful whitespace changes.
func verifyDispatchRebase(ctx context.Context, g board.DispatchIntegration) error {
	diff := func(base, target string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", "-C", g.CWD, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--binary", "--full-index", "--no-renames", base, target, "--")
		raw, err := cmd.Output()
		if err != nil {
			return "", launchError("board.dispatch_evidence_invalid", "rebase diff: "+err.Error())
		}
		text := integrationIndex.ReplaceAllString(string(raw), "")
		return integrationHunk.ReplaceAllString(text, "@@ -x${1} +x${2} @@"), nil
	}
	before, err := diff(g.ReviewBase, g.ReviewTarget)
	if err != nil {
		return err
	}
	after, err := diff(g.RebasedBase, g.SourceCommit)
	if err != nil {
		return err
	}
	if before != after {
		return launchError("board.dispatch_evidence_invalid", "rebased delivery does not preserve the reviewed patch")
	}
	return nil
}
