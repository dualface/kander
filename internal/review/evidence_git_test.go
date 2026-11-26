package review

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceRangesMatchOriginalCommands(t *testing.T) {
	repo := t.TempDir()
	gitRepo(t, repo, "init", "-q")
	gitRepo(t, repo, "config", "core.quotePath", "false")
	base := commitFile(t, repo, "a.txt", "base\n", "base")
	reviewed := commitFile(t, repo, "résumé.txt", "first diff body\n", "first")
	fixed := commitFile(t, repo, "résumé.txt", "fix diff body\n", "fixed")
	for _, tc := range []struct{ name, commit, reviewed string }{
		{"first", reviewed, ""},
		{"incremental", fixed, reviewed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := reviewContext{root: repo, base: base, commit: tc.commit, reviewed: tc.reviewed}
			runtime := t.TempDir()
			path := filepath.Join(runtime, "evidence.txt")
			if _, err := writeEvidence(ctx, runtime, path); err != nil {
				t.Fatal(err)
			}
			if got, want := readFile(t, path), expectedRangeEvidence(t, ctx); got != want {
				t.Fatalf("wrong evidence\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

// The remaining sections are byte-for-byte the Git output of d786183b,
// with the original framing and a single new explanation line.
func expectedRangeEvidence(t *testing.T, ctx reviewContext) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("Review range: " + ctx.base + ".." + ctx.commit + "\n")
	if ctx.reviewed != "" {
		out.WriteString("Incremental re-review; fix range: " + ctx.reviewed + ".." + ctx.commit + "\n")
	}
	out.WriteString("Diffs are not included. Inspect them with read-only Git in the review worktree; see the review contract.\n")
	reviewRange := ctx.base + ".." + ctx.commit
	out.WriteString("\n=== COMMITS ===\n")
	out.WriteString(evidenceTestGit(t, ctx.root, "log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", reviewRange))
	out.WriteString("\n=== FILE LEDGER ===\n")
	out.WriteString(evidenceTestGit(t, ctx.root, "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", reviewRange))
	if ctx.reviewed != "" {
		fixRange := ctx.reviewed + ".." + ctx.commit
		out.WriteString("\n=== FIX RANGE COMMITS ===\n")
		out.WriteString(evidenceTestGit(t, ctx.root, "log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", fixRange))
		out.WriteString("\n=== FIX RANGE FILE LEDGER ===\n")
		out.WriteString(evidenceTestGit(t, ctx.root, "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", fixRange))
	}
	return out.String()
}

func evidenceTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
