package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
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

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("KANDER_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with KANDER_UPDATE_GOLDEN=1 to create)", name, err)
	}
	if string(want) != got {
		t.Fatalf("%s drifted from the golden file:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
	if strings.Contains(got, "{{") {
		t.Fatalf("%s left a template action unrendered:\n%s", name, got)
	}
}

func dispatchFor(kind string) board.Dispatch {
	var d board.Dispatch
	d.Input.TaskID = "20260101-golden-task"
	d.Input.ID = "dispatch-1"
	d.Input.Kind = kind
	d.Authorization.Epoch = 2
	return d
}

// Every prompt Kander hands to an agent is rendered from the templates with fixed data and
// compared with its golden file. The PhaseSnippets extension point renders empty in this card.
func TestPromptGoldens(t *testing.T) {
	card := "- LANGUAGE: en\n- SIZE: small\n"
	for _, reporting := range []bool{false, true} {
		suffix := "-no-reporting"
		if reporting {
			suffix = "-reporting"
		}
		t.Run(strings.TrimPrefix(suffix, "-"), func(t *testing.T) {
			goldenConfig(t, reporting)
			start, err := startAgentPrompt("20260101-golden-task", goldenPaths, "", card)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, "start-single"+suffix, start)
			if reporting {
				if !strings.Contains(start, "KANDER-REPORTING-RULES.md") {
					t.Fatal("reporting reminder missing")
				}
			} else if strings.Contains(start, "KANDER-REPORTING-RULES.md") {
				t.Fatal("reporting reminder must be absent")
			}
		})
	}
	goldenConfig(t, false)
	groupStart, err := startAgentPrompt("20260101-golden-task", goldenPaths, "20260101-golden-group", card)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "start-group", groupStart)
	resume, err := resumeAgentPrompt("20260101-golden-task", "Fix the failing test.", goldenPaths, "working", card)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "resume-working", resume)
	review, err := resumePrompt("20260101-golden-task", "Fix the failing test.", goldenPaths, "review", card)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "resume-review", review)
	if !strings.Contains(review, "kander move 20260101-golden-task working") {
		t.Fatal("review card resume must ask for the move back to working first")
	}
	takeover, err := takeoverAgentPrompt("20260101-golden-task", "Fix the failing test.", goldenPaths, "codex", "working", card)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "takeover", takeover)
	orchestrate, err := orchestratorAgentPrompt([]OrchestrateTask{
		{TaskID: "20260101-golden-task", State: "todo", TaskGroup: "20260101-golden-group"},
		{TaskID: "20260101-golden-second-task", State: "backlog"},
	}, []string{card, card}, "Run the first card first.", goldenPaths)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "orchestrate", orchestrate)
	for _, kind := range []string{"fix", "sync", "wrap-up"} {
		instruction, err := DispatchInstruction(goldenPaths, dispatchFor(kind))
		if err != nil {
			t.Fatal(err)
		}
		assertGolden(t, "dispatch-"+kind, instruction)
	}
	report, err := config.RenderRulesTemplate(goldenPaths, "templates/report/completion.md", map[string]string{
		"TaskID": "20260101-golden-task", "Title": "Golden {{.X}} title", "Path": "/board/done/20260101-golden-task", "FinalState": "done",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "completion-report", strings.ReplaceAll(report, "{{.X}}", "<<X>>"))
	if !strings.Contains(report, "Golden {{.X}} title") {
		t.Fatal("data must not be parsed as template text")
	}
}

// Every prompt body template carries the PhaseSnippets extension point, and this card renders it
// empty: the rendered prompts equal the same templates rendered with PhaseSnippets removed.
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

// A broken installed template fails the prompt instead of silently dropping the fragment.
func TestBrokenInstalledTemplateFailsPrompt(t *testing.T) {
	goldenConfig(t, false)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(paths.RulesDir, "templates", "prompt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "templates", "prompt", "language.md"), []byte("{{.Missing}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	card := "- LANGUAGE: en\n- SIZE: small\n"
	if _, err := startAgentPrompt("20260101-golden-task", paths, "", card); err == nil {
		t.Fatal("start prompt must fail on a broken installed template")
	}
	if _, err := resumePrompt("20260101-golden-task", "go", paths, "review", card); err == nil {
		t.Fatal("resume prompt must fail on a broken installed template")
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, "templates", "prompt", "head.md"), []byte("{{.Missing}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := headedTaskFileInstruction(paths, "start", "20260101-golden-task", "", 0, "/tmp/task.md"); err == nil {
		t.Fatal("task-file instruction must fail on a broken head template")
	}
	if _, err := DispatchInstruction(config.InstallPaths{Mode: config.ModeGlobal, RulesDir: paths.RulesDir}, dispatchFor("fix")); err != nil {
		t.Fatalf("dispatch template untouched, must still render: %v", err)
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
