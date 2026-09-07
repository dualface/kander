package review

import (
	"context"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/board"
)

func verifyMechanicalGit(cwd string, v board.ReviewBatchView, r board.ReviewCloseRequest) ([]board.ReviewMechanicalGit, error) {
	return verifyMechanicalGitContext(context.Background(), cwd, v, r)
}

func verifyMechanicalGitContext(ctx context.Context, cwd string, v board.ReviewBatchView, r board.ReviewCloseRequest) ([]board.ReviewMechanicalGit, error) {
	claims, err := board.ReviewMechanicalClaims(v, r)
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		// Literal pathspecs keep a caller's path from widening its declared scope.
		paths := make([]string, len(c.Paths))
		for i, name := range c.Paths {
			paths[i] = ":(literal)" + name
		}
		args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", c.From, c.To, "--"}, paths...)
		out, _, code, err := gitCommandContext(ctx, args, cwd, "")
		if err != nil {
			return nil, err
		}
		actual := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
		slices.Sort(actual)
		if code != 0 || !slices.Equal(actual, c.Paths) {
			return nil, archiveError("mechanical scope must contain exactly changed files")
		}
		args = append([]string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", "--no-color", c.From, c.To, "--"}, paths...)
		out, _, code, err = gitCommandContext(ctx, args, cwd, "")
		if err != nil {
			return nil, err
		}
		if code != 0 || out == "" || board.ReviewDigest([]byte(out)) != c.DiffHash {
			return nil, archiveError("mechanical verification diff hash mismatch")
		}
	}
	return claims, nil
}
