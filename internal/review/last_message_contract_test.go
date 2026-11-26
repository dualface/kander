package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

const (
	baselineBase     = "1111111111111111111111111111111111111111"
	baselineReviewed = "2222222222222222222222222222222222222222"
	baselineCommit   = "3333333333333333333333333333333333333333"
)

// contractVariants builds the contract inputs the baseline files cover: a full
// PMQA review, and an incremental Security review with a report language.
func contractVariants(t *testing.T, agent string) map[string]reviewContext {
	t.Helper()
	settings, err := agentSettingsFor(agent, "PMQA", "large")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]reviewContext{
		"full-PMQA": {agent: agent, settings: settings, role: "PMQA", base: baselineBase, commit: baselineCommit},
		"incremental-Security-zh": {
			agent: agent, settings: settings, role: "Security", base: baselineBase, commit: baselineCommit,
			reviewed: baselineReviewed, reportLanguage: "zh-CN",
		},
	}
}

func isolateBuiltinConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
}

// writeUserConfig writes a scope configuration whose agents section is agents.
func writeUserConfig(t *testing.T, agents map[string]any) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["agents"] = agents
	if data, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// jsonObjectOf converts a definition part to the generic form a configuration file holds.
func jsonObjectOf(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// reviewOverlay is the args and review of a built-in agent in the form a user
// definition declares them; the loader requires the two together.
func reviewOverlay(t *testing.T, agent string, review map[string]any) map[string]any {
	t.Helper()
	def := config.AgentFor(nil, agent)
	if review == nil {
		review = jsonObjectOf(t, def.Review)
	}
	return map[string]any{"args": jsonObjectOf(t, def.Args), "review": review}
}

func TestBuiltinReviewContractsMatchBaseline(t *testing.T) {
	isolateBuiltinConfig(t)
	dir := filepath.Join("testdata", "review-contract")
	agents := config.ReviewAgentNames(nil)
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2*len(agents) {
		t.Fatalf("%d baseline files for %d built-in reviewers", len(files), len(agents))
	}
	for _, agent := range agents {
		for variant, ctx := range contractVariants(t, agent) {
			want, err := os.ReadFile(filepath.Join(dir, agent+"-"+variant+".md"))
			if err != nil {
				t.Fatal(err)
			}
			if got := mustBuildContract(t, ctx); got != string(want) {
				t.Errorf("%s %s: the review contract differs from the baseline", agent, variant)
			}
		}
	}
}

func TestUserReviewOverrideKeepsTheOutputContract(t *testing.T) {
	review := jsonObjectOf(t, config.AgentFor(nil, "claude").Review)
	review["inspection"] = "Inspect read-only, as the user wrote."
	writeUserConfig(t, map[string]any{"claude": reviewOverlay(t, "claude", review)})
	ctx := contractVariants(t, "claude")["full-PMQA"]
	if !strings.Contains(ctx.settings.inspectionRules, "as the user wrote") {
		t.Fatalf("the user review definition was not applied: %q", ctx.settings.inspectionRules)
	}
	if !strings.Contains(mustBuildContract(t, ctx), mustLastMessageContract(t)) {
		t.Fatal("overriding agents.claude.review dropped the output contract")
	}
}

func TestCustomAgentWithClaudeDialectGetsNoOutputContract(t *testing.T) {
	custom := reviewOverlay(t, "claude", nil)
	custom["dialect"] = "claude"
	custom["path"] = "sh"
	writeUserConfig(t, map[string]any{"myreviewer": custom})
	if config.EmbeddedReviewLastMessageReport("myreviewer") {
		t.Fatal("a custom agent inherited last_message_report through its dialect")
	}
	ctx := contractVariants(t, "myreviewer")["full-PMQA"]
	if strings.Contains(mustBuildContract(t, ctx), mustLastMessageContract(t)) {
		t.Fatal("a custom agent with dialect claude got the output contract")
	}
}

func TestUserLastMessageReportIsIgnoredAndHasNoEffect(t *testing.T) {
	codex := jsonObjectOf(t, config.AgentFor(nil, "codex").Review)
	codex["last_message_report"] = true
	claude := jsonObjectOf(t, config.AgentFor(nil, "claude").Review)
	claude["last_message_report"] = false
	writeUserConfig(t, map[string]any{
		"codex":  reviewOverlay(t, "codex", codex),
		"claude": reviewOverlay(t, "claude", claude),
	})
	cfg, err := config.Effective(nil)
	if err != nil {
		t.Fatalf("a configuration that writes last_message_report must stay valid: %v", err)
	}
	var ignored []string
	for _, item := range cfg.Ignored {
		ignored = append(ignored, item.PathString())
	}
	slices.Sort(ignored)
	want := []string{"agents.claude.review.last_message_report", "agents.codex.review.last_message_report"}
	if !slices.Equal(ignored, want) {
		t.Fatalf("ignored = %v, want %v", ignored, want)
	}
	marker := mustLastMessageContract(t)
	if strings.Contains(mustBuildContract(t, contractVariants(t, "codex")["full-PMQA"]), marker) {
		t.Fatal("a user last_message_report added the output contract to codex")
	}
	if !strings.Contains(mustBuildContract(t, contractVariants(t, "claude")["full-PMQA"]), marker) {
		t.Fatal("a user last_message_report=false removed the output contract from claude")
	}
}
