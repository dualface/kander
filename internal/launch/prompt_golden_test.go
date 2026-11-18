package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// goldenPaths keeps every rendered path deterministic; the rules root does not exist, so the
// embedded templates render.
var goldenPaths = config.InstallPaths{Mode: config.ModeGlobal, RulesDir: "/rules"}

func goldenConfig(t *testing.T, reporting bool) {
	t.Helper()
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.Rules[config.RuleReporting] = reporting
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// Every prompt body template carries the PhaseSnippets extension point.
func TestPromptTemplatesCarryPhaseSnippetsPlaceholder(t *testing.T) {
	for _, name := range []string{"start.md", "resume.md", "takeover.md", "orchestrate.md", "dispatch.md"} {
		data, err := config.ReadRulesFile(goldenPaths, "templates/prompt/"+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "{{.PhaseSnippets}}") {
			t.Fatalf("%s has no PhaseSnippets extension point", name)
		}
	}
}

// An installed template wins over the embedded copy: editing it changes the next prompt.
func TestInstalledPromptTemplateOverridesEmbedded(t *testing.T) {
	goldenConfig(t, false)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(paths.RulesDir, "templates", "prompt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "templates", "prompt", "start.md"), []byte("LOCAL START {{.TaskID}} {{.Preamble}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	start, err := startAgentPrompt("20260101-golden-task", paths, "", "- LANGUAGE: en\n- SIZE: small\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(start, "LOCAL START 20260101-golden-task First read") {
		t.Fatalf("installed template not used: %s", start)
	}
}

// The rollout prefixes used to find a Codex session come from the same installed head template
// the launch wrote with, so a customized head still matches.
func TestPromptPrefixesFollowInstalledHead(t *testing.T) {
	goldenConfig(t, false)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(paths.RulesDir, "templates", "prompt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "templates", "prompt", "head.md"), []byte("Custom {{.Kind}} {{.TaskID}}."), 0o600); err != nil {
		t.Fatal(err)
	}
	old := currentInstallPaths
	currentInstallPaths = func() (config.InstallPaths, error) { return paths, nil }
	t.Cleanup(func() { currentInstallPaths = old })
	prefixes := codexPromptPrefixes("task-9")
	if !startsWithAny("Custom start task-9; full instructions", prefixes) || !startsWithAny("执行 Kanban 任务 task-9.", prefixes) {
		t.Fatalf("prefixes=%v", prefixes)
	}
}
