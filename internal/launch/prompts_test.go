package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestAgentPromptsLoadConfigurationBeforeCommandContract(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: filepath.Join(t.TempDir(), "project"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		RulesDir:    filepath.Join(t.TempDir(), "rules"),
	}
	want := filepath.Join(paths.RulesDir, "KANDER-KANBAN-RULES.md")
	bootstrap := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	configuration := filepath.Join(paths.BinDir, "kander") + " config --json"
	prompts := []struct {
		name string
		make func() (string, error)
	}{
		{"start", func() (string, error) { return startAgentPrompt("task-1", paths, "") }},
		{"resume", func() (string, error) { return resumeAgentPrompt("task-1", "继续", paths, "working") }},
		{"notify-resume", func() (string, error) { return resumePrompt("task-1", "继续", paths, "review") }},
		{"takeover", func() (string, error) { return takeoverAgentPrompt("task-1", "继续", paths, "codex", "review") }},
	}
	for _, lang := range []string{"cn", "en"} {
		for _, tc := range prompts {
			t.Run(lang+"/"+tc.name, func(t *testing.T) {
				t.Setenv(config.EnvLang, lang)
				prompt, err := tc.make()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt does not contain %q: %s", want, prompt)
				}
				if strings.Contains(prompt, commandName(paths)+" rules") {
					t.Fatalf("prompt still invokes rules command: %s", prompt)
				}
				if strings.Index(prompt, bootstrap) < 0 || strings.Index(prompt, configuration) < strings.Index(prompt, bootstrap) || strings.Index(prompt, want) < strings.Index(prompt, configuration) {
					t.Fatalf("bootstrap/configuration/contract order is wrong: %s", prompt)
				}
				for _, requirement := range []string{"提交并 push", "合回 develop", "进入任务 worktree", "补充任务分支", "commit and push", "merge back into develop", "enter the task worktree", "record the task branch"} {
					if strings.Contains(prompt, requirement) {
						t.Fatalf("unconditional workflow requirement %q: %s", requirement, prompt)
					}
				}
			})
		}
	}
}

func TestLocalizedPromptsAndSessionLookup(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	for _, lang := range []string{"cn", "en"} {
		t.Run(lang, func(t *testing.T) {
			t.Setenv(config.EnvLang, lang)
			message := "user input {{.V0}} 100% <&>"
			normal, err := resumePrompt("task-1", message, paths, "working")
			if err != nil {
				t.Fatal(err)
			}
			pending, err := resumePrompt("task-1", message, paths, "review")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(normal, message) || !strings.Contains(pending, message) {
				t.Fatal("message interpolation changed user input")
			}
			selfMove := "kander move task-1 working"
			if !strings.Contains(pending, selfMove) || strings.Contains(normal, selfMove) {
				t.Fatalf("review state must require self move, working must not: %s", pending)
			}
			if lang == "en" && !strings.HasPrefix(normal, "Resume Kanban task task-1.") {
				t.Fatalf("English prompt: %s", normal)
			}
			if lang == "cn" && !strings.HasPrefix(normal, "继续 Kanban 任务 task-1.") {
				t.Fatalf("Chinese prompt: %s", normal)
			}
			// Both current languages and the old mixed-language file pointer remain searchable.
			for _, prefix := range []string{
				"执行 Kanban 任务 task-1; full instructions are in the UTF-8 task file at /tmp/task.md",
				"Resume Kanban task task-1; full instructions are in the UTF-8 task file at /tmp/task.md",
				"接管 Kanban 任务 task-1.",
				"Take over Kanban task task-1.",
			} {
				if !startsWithAny(prefix, codexPromptPrefixes("task-1")) {
					t.Errorf("session not matched: %s", prefix)
				}
				if startsWithAny(prefix, codexPromptPrefixes("task-10")) {
					t.Errorf("wrong task matched: %s", prefix)
				}
			}
		})
	}
}
