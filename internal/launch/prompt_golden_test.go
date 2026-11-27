package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// goldenPaths keeps every rendered path deterministic; the rules root does not exist, so the
// embedded templates render.
var goldenPaths = config.InstallPaths{RulesDir: "/rules"}

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
	paths := config.InstallPaths{RulesDir: t.TempDir()}
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

// waitingReminder is the embedded text of templates/prompt/waiting.md, which every execution
// prompt carries whatever the module switches say.
func waitingReminder(t *testing.T) string {
	t.Helper()
	data, err := config.ReadRulesFile(goldenPaths, "templates/prompt/waiting.md", nil)
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded waiting template: %q %v", data, err)
	}
	return string(data)
}

// The reporting switch controls only the report reminder in the start and takeover prompts.
// With reporting off, the instruction contains the preamble, waiting reminder, and SIZE sentence
// in order. The waiting reminder is present in both cases.
func TestExecutionPromptsCarryTheWaitingReminder(t *testing.T) {
	waiting := waitingReminder(t)
	card := "- LANGUAGE: en\n- SIZE: small\n"
	preamble, err := rulesAndLanguage(goldenPaths, "en")
	if err != nil {
		t.Fatal(err)
	}
	sizeText, err := render(goldenPaths, "size.md", map[string]string{"Size": "small"})
	if err != nil {
		t.Fatal(err)
	}
	withoutReport := preamble + " " + waiting + " " + sizeText
	for _, reporting := range []bool{true, false} {
		goldenConfig(t, reporting)
		prompts := map[string]func() (string, error){
			"start": func() (string, error) { return startAgentPrompt("20260101-golden-task", goldenPaths, "", card) },
			"takeover": func() (string, error) {
				return takeoverAgentPrompt("20260101-golden-task", "continue", goldenPaths, "codex", "working", card)
			},
		}
		for name, build := range prompts {
			text, err := build()
			if err != nil {
				t.Fatal(err)
			}
			if !reporting && !strings.Contains(text, withoutReport) {
				t.Fatalf("%s prompt without reporting lacks the preamble, waiting reminder, or SIZE sentence in order: %s", name, text)
			}
			if !strings.Contains(text, waiting+" "+sizeText) {
				t.Fatalf("%s prompt (reporting=%v) lacks the waiting reminder before the SIZE sentence: %s", name, reporting, text)
			}
			if reportingIndex := strings.Index(text, "KANDER-REPORTING-RULES.md"); reporting != (reportingIndex >= 0) || reporting && reportingIndex > strings.Index(text, waiting) {
				t.Fatalf("%s prompt (reporting=%v) orders the report and waiting reminders wrongly: %s", name, reporting, text)
			}
		}
	}
}

// The orchestrate prompt carries its own reminder: the wake-up rule of subscribe.md, the way each
// kind of subscription wakes the session, and the long-command rule.
func TestOrchestratePromptCarriesTheWaitingReminder(t *testing.T) {
	goldenConfig(t, false)
	text, err := orchestratorAgentPrompt([]OrchestrateTask{{TaskID: "20260101-golden-task", State: "todo"}}, []string{"- LANGUAGE: en\n- SIZE: small\n"}, "plan notes", goldenPaths)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Never end a turn without a wake-up source per ` + "`snippets/commands/subscribe.md`" + ` "Waking Up"`,
		"wait in its subscription loop, because a background `kander subscribe` wakes this session only through a report the runner delivers (its exit by default, each output line under `--follow`).",
		"Run long commands such as `kander review` in the foreground or in a background mode whose completion the runner reports; never detach them with & or nohup.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("orchestrate prompt lacks %q: %s", want, text)
		}
	}
}

// An installation from before the waiting reminder has no waiting.md: the prompt still builds and
// carries the embedded sentence, while the installed templates keep winning.
func TestOldInstallWithoutWaitingTemplateUsesEmbedded(t *testing.T) {
	goldenConfig(t, false)
	paths := config.InstallPaths{RulesDir: t.TempDir()}
	dir := filepath.Join(paths.RulesDir, "templates", "prompt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "size.md"), []byte("OLD SIZE {{.Size}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	start, err := startAgentPrompt("20260101-golden-task", paths, "", "- LANGUAGE: en\n- SIZE: small\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start, waitingReminder(t)+" OLD SIZE small") {
		t.Fatalf("embedded waiting reminder missing before the installed size sentence: %s", start)
	}
}

// The rollout prefixes used to find a Codex session come from the same installed head template
// the launch wrote with, so a customized head still matches.
func TestPromptPrefixesFollowInstalledHead(t *testing.T) {
	goldenConfig(t, false)
	paths := config.InstallPaths{RulesDir: t.TempDir()}
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
