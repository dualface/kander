package review

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestReviewGitInspectionIgnoresUserOverlaysAndDialect(t *testing.T) {
	agy := jsonObjectOf(t, config.AgentFor(nil, "agy").Review)
	agy["git_inspection"] = true
	codex := jsonObjectOf(t, config.AgentFor(nil, "codex").Review)
	codex["git_inspection"] = false
	custom := reviewOverlay(t, "agy", nil)
	custom["dialect"] = "agy"
	custom["path"] = "git"
	writeUserConfig(t, map[string]any{
		"agy": reviewOverlay(t, "agy", agy), "codex": reviewOverlay(t, "codex", codex), "myreviewer": custom,
	})
	cfg, err := config.Effective(nil)
	if err != nil {
		t.Fatal(err)
	}
	var ignored []string
	for _, item := range cfg.Ignored {
		ignored = append(ignored, item.PathString())
	}
	slices.Sort(ignored)
	if want := []string{"agents.agy.review.git_inspection", "agents.codex.review.git_inspection"}; !slices.Equal(ignored, want) {
		t.Fatalf("ignored keys = %v, want %v", ignored, want)
	}
	for _, agent := range []string{"agy", "codex", "myreviewer"} {
		if !config.HasReviewTemplate(cfg, agent) {
			t.Errorf("%s has no usable review definition", agent)
		}
		if got := config.EmbeddedReviewGitInspection(agent); got != (agent != "agy") {
			t.Errorf("%s has unexpected Git inspection capability %v", agent, got)
		}
	}
}

func TestReviewGitInspectionContract(t *testing.T) {
	isolateBuiltinConfig(t)
	ctx := promptContractContext("codex")
	for _, reviewed := range []string{"", baselineReviewed} {
		ctx.reviewed = reviewed
		contract := mustBuildContract(t, ctx)
		for _, want := range []string{
			"git ls-tree -r --name-only <commit>",
			"It does not contain diffs",
			"worktree named in the bootstrap",
			"git -C \"<worktree>\"",
			"first review inspect `<base>..<commit>`",
			"fix range `<reviewed>..<commit>`",
			"Always pass `--no-ext-diff --no-textconv` to `git diff`, `git log`, and `git show`",
			"--stat <range>` or use `--numstat`",
			"git cat-file -s <commit>:<path>",
			"':(exclude)<path>'",
			"inspect only relevant hunks or truncate long lines",
			"Run only Git commands that do not write",
			"dubious ownership",
			"git -c safe.directory=\"<worktree>\"",
			"If Git remains unavailable, say so in the report and mark any conclusion that depends on the diff Unverifiable",
		} {
			if !strings.Contains(contract, want) {
				t.Errorf("reviewed=%q: contract lacks %q", reviewed, want)
			}
		}
		for _, removed := range []string{"COMMIT TREE", "OMITTED DIFFS", "the patch as navigation"} {
			if strings.Contains(contract, removed) {
				t.Errorf("reviewed=%q: contract still contains %q", reviewed, removed)
			}
		}
	}
}

func TestPreviouslyInstalledReviewContractRendersWithoutOmissionSection(t *testing.T) {
	isolateBuiltinConfig(t)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	filename := config.ReviewContractFilename
	dir := filepath.Join(paths.RulesDir, reviewTemplatesDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := readFile(t, filepath.Join("testdata", "installed-41694947", filename))
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	contract := mustBuildContract(t, promptContractContext("codex"))
	if !strings.Contains(contract, "The COMMIT TREE in the evidence file") {
		t.Fatal("expected the installed template to be used")
	}
	if strings.Contains(contract, "omitted or shown only as previews") {
		t.Fatal("the legacy omission section was rendered")
	}
}
