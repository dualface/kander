package review

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestEvidenceUnshortenedMatchesOriginalCommands(t *testing.T) {
	repo := t.TempDir()
	gitRepo(t, repo, "init", "-q")
	base := commitFile(t, repo, "a", "a\n", "base")
	reviewed := commitFile(t, repo, "a", "b\n", "first")
	commit := commitFile(t, repo, "b", "c\n", "fixed")
	for _, fix := range []string{"", reviewed} {
		ctx := reviewContext{root: repo, base: base, reviewed: fix, commit: commit}
		got, result := writeTestEvidence(t, ctx)
		want := originalEvidence(t, ctx)
		if result.omitted != 0 || got != want {
			t.Fatalf("small evidence changed (reviewed %q)\nwant:\n%s\ngot:\n%s", fix, want, got)
		}
	}
}

// originalEvidence reconstructs the original evidence with the unmodified
// Git arguments, including exact headings and blank lines.
func originalEvidence(t *testing.T, ctx reviewContext) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("Review range: " + ctx.base + ".." + ctx.commit + "\n")
	if ctx.reviewed != "" {
		out.WriteString("Incremental re-review; fix range: " + ctx.reviewed + ".." + ctx.commit + "\n")
	}
	run := func(name string, args ...string) {
		t.Helper()
		out.WriteString("\n=== " + name + " ===\n")
		out.WriteString(evidenceTestGit(t, ctx.root, args))
	}
	rangeSections := func(from, prefix string) {
		rangeSpec := from + ".." + ctx.commit
		run(prefix+"COMMITS", "log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", rangeSpec)
		run(prefix+"FILE LEDGER", "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", rangeSpec)
		run(prefix+"PATCH", "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--patch", rangeSpec)
	}
	rangeSections(ctx.base, "")
	run("COMMIT TREE", "ls-tree", "-r", ctx.commit)
	if ctx.reviewed != "" {
		rangeSections(ctx.reviewed, "FIX RANGE ")
	}
	return out.String()
}

func evidenceTestGit(t *testing.T, root string, args []string) string {
	t.Helper()
	result, err := evidenceGit(root, args)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEvidenceGitPathCommands(t *testing.T) {
	for _, tc := range []struct {
		name, path, rename string
	}{
		{name: "space", path: "space a's.txt"},
		{name: "renamed", path: "input with ' quote.txt", rename: "output name.txt"},
		{name: "non UTF-8", path: "bad\xff.txt"},
		{name: "control byte", path: "new\nline.txt"},
		{name: "literal wildcard", path: "a[1].txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			gitRepo(t, root, "init", "-q")
			gitRepo(t, root, "config", "core.autocrlf", "false")
			shared := strings.Repeat(strings.Repeat("base", 100)+"\n", 4000)
			line := strings.Repeat("x", evidenceFileLimit) + "\n"
			path := filepath.Join(root, tc.path)
			if err := os.WriteFile(path, []byte(shared+line), 0o600); err != nil {
				if tc.name == "space" {
					t.Fatal(err)
				}
				t.Skipf("filesystem does not support the test path %q: %v", tc.path, err)
			}
			if tc.name == "non UTF-8" {
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == tc.path }) {
					t.Skip("filesystem does not preserve the raw non-UTF-8 path bytes")
				}
			}
			gitRepo(t, root, "add", "-A")
			gitRepo(t, root, "commit", "-qm", "base")
			base := gitRepo(t, root, "rev-parse", "HEAD")
			newPath := tc.path
			if tc.rename != "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				newPath = tc.rename
			}
			commit := commitFile(t, root, newPath, shared+strings.Replace(line, "x", "y", 1), "change")
			if tc.rename != "" {
				gitRepo(t, root, "add", "-A")
				gitRepo(t, root, "commit", "--amend", "--no-edit", "-q")
				commit = gitRepo(t, root, "rev-parse", "HEAD")
			}
			ctx := reviewContext{root: root, base: base, commit: commit}
			evidence, result := writeTestEvidence(t, ctx)
			item := omittedEvidence(t, evidence)
			pathField := evidenceField(item, "path")
			if result.omitted != 1 || pathField != evidenceJSONString(newPath) || evidenceField(item, "shown") != "preview" ||
				evidenceField(item, "metadata") != "" || evidenceField(item, "numstat") != "1\t1" {
				t.Fatalf("wrong omission (%+v): %s", result, item)
			}
			if tc.rename != "" && (!strings.HasPrefix(evidenceField(item, "status"), "R") || evidenceField(item, "old_path") != evidenceJSONString(tc.path)) {
				t.Fatalf("missing rename metadata: %s", item)
			}
			if tc.name == "non UTF-8" {
				decoded, err := base64.StdEncoding.DecodeString(evidenceField(item, "path_base64"))
				if err != nil || string(decoded) != tc.path || !strings.Contains(pathField, "�") {
					t.Fatalf("lost raw path bytes: %s (%v)", item, err)
				}
			}
			args := evidenceCommandArgs(t, evidenceField(item, "command"))
			got := evidenceTestGit(t, root, args)
			if tc.name == "control byte" {
				before, after := evidenceField(item, "blob_before"), evidenceField(item, "blob_after")
				if !slices.Equal(args, []string{"diff", before, after}) || !strings.Contains(got, "@@ ") {
					t.Fatalf("not a usable blob comparison: %q", args)
				}
			} else {
				want := evidenceTestGit(t, root, evidenceDiffArgs("--patch", base+".."+commit))
				if got != want {
					t.Fatalf("regenerated %d of %d original bytes with %q", len(got), len(want), args)
				}
			}
		})
	}
}

func TestEvidenceGitTypeChangeWithLargeFile(t *testing.T) {
	root := t.TempDir()
	gitRepo(t, root, "init", "-q")
	gitRepo(t, root, "config", "core.symlinks", "true")
	link := filepath.Join(root, "link")
	if err := os.Symlink("destination", link); err != nil {
		t.Skipf("filesystem does not support symlinks: %v", err)
	}
	gitRepo(t, root, "add", "link")
	large := strings.Repeat("x", evidenceFileLimit) + "\n"
	base := commitFile(t, root, "large", large, "base")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("regular file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, root, "add", "link")
	commit := commitFile(t, root, "large", "y"+large[1:], "type change and large file")
	ctx := reviewContext{root: root, base: base, commit: commit}
	got, result := writeTestEvidence(t, ctx)
	item := omittedEvidence(t, got)
	if result.omitted != 1 || strings.Count(got, "diff --git a/link b/link\n") != 2 ||
		evidenceField(item, "path") != `"large"` || evidenceField(item, "numstat") != "1\t1" ||
		evidenceField(item, "blob_before") != gitRepo(t, root, "rev-parse", base+":large") ||
		evidenceField(item, "blob_after") != gitRepo(t, root, "rev-parse", commit+":large") {
		t.Fatalf("incorrect T grouping or large-file metadata: %s", item)
	}
	sections, err := evidenceRange(func(args []string) (string, error) { return evidenceGit(root, args) }, base, commit, "")
	if err != nil {
		t.Fatal(err)
	}
	patch := sections[len(sections)-1].patch
	if len(patch.units) != 2 {
		t.Fatalf("expected two units, got %d", len(patch.units))
	}
	for _, unit := range patch.units {
		if unit.path != "link" {
			continue
		}
		if unit.metadata == nil || unit.metadata.status != "T" || len(unit.blocks) != 2 {
			t.Fatal("type change was not resolved as one T unit")
		}
		cmd := evidenceField(unit.omittedRepresentation("total budget", "").item, "command")
		if evidenceTestGit(t, root, evidenceCommandArgs(t, cmd)) != strings.Join(unit.blocks, "") {
			t.Fatal("T command did not reproduce both blocks")
		}
		return
	}
	t.Fatal("missing type change")
}

func TestEvidenceGitConfiguration(t *testing.T) {
	root := t.TempDir()
	gitRepo(t, root, "init", "-q")
	gitRepo(t, root, "config", "diff.noprefix", "true")
	gitRepo(t, root, "config", "diff.submodule", "log")
	gitRepo(t, root, "config", "core.quotePath", "false")
	pointer1 := commitFile(t, root, "history", "one\n", "first submodule pointer")
	pointer2 := commitFile(t, root, "history", "two\n", "next submodule pointer")
	gitRepo(t, root, "update-index", "--add", "--cacheinfo", "160000,"+pointer1+",sub")
	const name = "猫.txt"
	large := strings.Repeat("a", evidenceFileLimit) + "\n"
	base := commitFile(t, root, name, large, "base")
	gitRepo(t, root, "update-index", "--cacheinfo", "160000,"+pointer2+",sub")
	commit := commitFile(t, root, name, "b"+large[1:], "changed")
	ctx := reviewContext{root: root, base: base, commit: commit}
	got, result := writeTestEvidence(t, ctx)
	again, againResult := writeTestEvidence(t, ctx)
	if got != again || result != againResult {
		t.Fatal("regenerating evidence changed its bytes")
	}
	item := omittedEvidence(t, got)
	if result.omitted != 1 || !strings.Contains(got, "diff --git a/sub b/sub\n") ||
		!strings.Contains(got, "-Subproject commit "+pointer1+"\n+Subproject commit "+pointer2+"\n") ||
		!strings.Contains(got, `diff --git "a/\347\214\253.txt" "b/\347\214\253.txt"`) ||
		evidenceField(item, "path") != `"猫.txt"` || evidenceField(item, "metadata") != "" {
		t.Fatalf("diff settings escaped normalization: %s", item)
	}
}

func TestEvidenceMetadataMismatchWarnsAndStaysBounded(t *testing.T) {
	setupLang(t, filepath.Join(t.TempDir(), "config.json"))
	root := t.TempDir()
	gitRepo(t, root, "init", "-q")
	name := "with space"
	large := strings.Repeat("a", evidenceFileLimit) + "\n"
	base := commitFile(t, root, name, large, "base")
	commit := commitFile(t, root, name, "b"+large[1:], "changed")
	original := evidenceTestGit(t, root, evidenceDiffArgs("--patch", base+".."+commit))
	calls := 0
	run := func(args []string) (string, error) {
		calls++
		if args[0] != "log" {
			for _, arg := range []string{"core.quotePath=true", "--submodule=short", "--src-prefix=a/", "--dst-prefix=b/"} {
				if !slices.Contains(args, arg) {
					t.Fatalf("Git evidence call lacks %q: %q", arg, args)
				}
			}
		}
		if slices.Contains(args, "--raw") || slices.Contains(args, "--numstat") {
			return "", nil
		}
		return evidenceGit(root, args)
	}
	stderr, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	old := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = old; _ = writer.Close() }()
	sections, err := evidenceRange(run, base, commit, "")
	os.Stderr = old
	_ = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	message, err := io.ReadAll(stderr)
	wantMessage := config.Text("review.evidence_metadata_unavailable", "PATCH", 1, 0)
	if err != nil || !strings.Contains(string(message), wantMessage) || calls != 5 {
		t.Fatalf("want localized mismatch warning and five calls, got %q, %d (%v)", message, calls, err)
	}

	evidence, result := renderEvidence("range\n", sections, testEvidenceLimits())
	item := omittedEvidence(t, evidence)
	patch, _, _ := strings.Cut(strings.Split(evidence, "\n=== PATCH ===\n")[1], omittedDiffsHeading)
	if result.omitted != 1 || len(evidence) > evidenceTotalLimit || strings.Count(strings.TrimRight(patch, "\n"), "\n") != 1 ||
		evidenceField(item, "shown") != "header-only" || evidenceField(item, "metadata") != "unavailable" ||
		evidenceField(item, "metadata_reason") == "" || evidenceField(item, "path") != `"with space"` {
		t.Fatalf("mismatch did not produce bounded evidence: %s", item)
	}
	for _, field := range []string{"old_path", "status", "numstat", "blob_before", "blob_after"} {
		if evidenceField(item, field) != "" {
			t.Errorf("unexpected %s: %s", field, item)
		}
	}
	if got := evidenceTestGit(t, root, evidenceCommandArgs(t, evidenceField(item, "command"))); got != original {
		t.Fatalf("fallback command regenerated %d of %d bytes", len(got), len(original))
	}
}

func omittedEvidence(t *testing.T, text string) string {
	t.Helper()
	_, item, found := strings.Cut(text, omittedDiffsHeading)
	if !found {
		t.Fatal("no omission index")
	}
	return item
}

func evidenceField(item, field string) string {
	for line := range strings.SplitSeq(item, "\n") {
		for _, indent := range []string{"  ", "- "} {
			if value, found := strings.CutPrefix(line, indent+field+": "); found {
				return value
			}
		}
	}
	return ""
}

// Parse the restricted POSIX quoting emitted by the evidence writer. Execute
// the resulting argv directly so tests don't depend on a shell.
func evidenceCommandArgs(t *testing.T, command string) []string {
	t.Helper()
	word, command, ok := strings.Cut(command, " ")
	if !ok || word != "git" {
		t.Fatalf("not a Git command: %q %q", word, command)
	}
	var args []string
	for command != "" {
		var arg strings.Builder
		for command != "" && command[0] != ' ' {
			switch {
			case command[0] == '\'':
				value, rest, closed := strings.Cut(command[1:], "'")
				if !closed {
					t.Fatal("unterminated quote in evidence command")
				}
				arg.WriteString(value)
				command = rest
			case strings.HasPrefix(command, "\\'"):
				arg.WriteByte('\'')
				command = command[2:]
			case command[0] != '\\':
				arg.WriteByte(command[0])
				command = command[1:]
			default:
				t.Fatalf("invalid quote in evidence command: %q", command)
			}
		}
		args = append(args, arg.String())
		command = strings.TrimLeft(command, " ")
	}
	return args
}

func TestEvidenceFallbackUsesStatForUnavailableCommands(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		parsed       bool
	}{
		{"control byte", "diff --git \"a/new\\nline\" \"b/new\\nline\"\n", true},
		{"ambiguous", "diff --git a/one b/two\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := tc.header + "@@ -1 +1 @@\n+" + strings.Repeat("x", evidenceFileLimit) + "\n"
			from, to := strings.Repeat("a", 40), strings.Repeat("b", 40)
			patch, _, _, _ := collectEvidencePatch("PATCH", from, to, block, "", "")
			out, result := renderEvidence("", []evidenceSection{{name: "PATCH", patch: patch}}, testEvidenceLimits())
			item := omittedEvidence(t, out)
			if result.omitted != 1 || evidenceField(item, "command") != "unavailable" ||
				(evidenceField(item, "path_parsed") == "false") == tc.parsed ||
				!slices.Equal(evidenceCommandArgs(t, evidenceField(item, "stat_command")), evidenceDiffArgs("--stat", from, to)) {
				t.Fatalf("expected stat-only inspection: %s", item)
			}
		})
	}
}

func TestEvidenceColorConfigKeepsFallbackBounded(t *testing.T) {
	setupLang(t, filepath.Join(t.TempDir(), "config.json"))
	root := t.TempDir()
	gitRepo(t, root, "init", "-q")
	gitRepo(t, root, "config", "color.diff", "always")
	large := strings.Repeat("a", evidenceFileLimit) + "\n"
	base := commitFile(t, root, "large", large, "base")
	commit := commitFile(t, root, "large", "b"+large[1:], "changed")
	text, result := writeTestEvidence(t, reviewContext{root: root, base: base, commit: commit})
	item := omittedEvidence(t, text)
	if result.omitted != 1 || len(text) > evidenceFileLimit || evidenceField(item, "path") != `"large"` ||
		evidenceField(item, "shown") != "header-only" {
		t.Fatalf("forced color escaped the budget: %s", item)
	}
}

func TestEvidenceControlPathWithOnlyOneBlob(t *testing.T) {
	root := t.TempDir()
	gitRepo(t, root, "init", "-q")
	base := commitFile(t, root, "base", "baseline\n", "base")
	const name = "new\nline"
	body := strings.Repeat("x", evidenceFileLimit) + "\n"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Skipf("filesystem does not support the test path: %v", err)
	}
	gitRepo(t, root, "add", "-A")
	gitRepo(t, root, "commit", "-qm", "add")
	added := gitRepo(t, root, "rev-parse", "HEAD")
	gitRepo(t, root, "rm", "--", name)
	gitRepo(t, root, "commit", "-qm", "delete")
	deleted := gitRepo(t, root, "rev-parse", "HEAD")
	for _, change := range []struct{ status, from, to, blob string }{
		{"A", base, added, "blob_after"},
		{"D", added, deleted, "blob_before"},
	} {
		t.Run(change.status, func(t *testing.T) {
			ctx := reviewContext{root: root, base: change.from, commit: change.to}
			got, result := writeTestEvidence(t, ctx)
			item := omittedEvidence(t, got)
			blob := evidenceField(item, change.blob)
			args := evidenceCommandArgs(t, evidenceField(item, "command"))
			if result.omitted != 1 || evidenceField(item, "status") != change.status ||
				!slices.Equal(args, []string{"cat-file", "blob", blob}) ||
				evidenceTestGit(t, root, args) != body {
				t.Fatalf("command does not return the only available blob: %s", item)
			}
		})
	}
}
