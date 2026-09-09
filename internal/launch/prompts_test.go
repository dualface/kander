package launch

import (
	"os"
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
	card := "- LANGUAGE: en\n"
	want := filepath.Join(paths.RulesDir, "KANDER-KANBAN-RULES.md")
	bootstrap := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	configuration := filepath.Join(paths.BinDir, "kander") + " config --json"
	prompts := []struct {
		name string
		make func() (string, error)
	}{
		{"start", func() (string, error) { return startAgentPrompt("task-1", paths, "", card) }},
		{"resume", func() (string, error) { return resumeAgentPrompt("task-1", "继续", paths, "working", card) }},
		{"notify-resume", func() (string, error) { return resumePrompt("task-1", "继续", paths, "review", card) }},
		{"takeover", func() (string, error) { return takeoverAgentPrompt("task-1", "继续", paths, "codex", "review", card) }},
	}
	for _, lang := range []string{"cn", "en", "ja"} {
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
				directive := promptLanguageDirective("en")
				ruleIdx := strings.Index(prompt, bootstrap)
				dirIdx := strings.Index(prompt, directive)
				if dirIdx < 0 || dirIdx < ruleIdx {
					t.Fatalf("language directive must follow rule loading: %s", prompt)
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
	card := "- LANGUAGE: zh-CN\n"
	for _, lang := range []string{"cn", "en", "ja"} {
		t.Run(lang, func(t *testing.T) {
			t.Setenv(config.EnvLang, lang)
			message := "user input {{.V0}} 100% <&>"
			normal, err := resumePrompt("task-1", message, paths, "working", card)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := resumePrompt("task-1", message, paths, "review", card)
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
			if lang == "ja" && !strings.HasPrefix(normal, "Kanban タスク task-1 を再開します.") {
				t.Fatalf("Japanese prompt: %s", normal)
			}
			// Every interface language and the old mixed-language file pointer remain searchable.
			for _, prefix := range []string{
				"执行 Kanban 任务 task-1; full instructions are in the UTF-8 task file at /tmp/task.md",
				"Resume Kanban task task-1; full instructions are in the UTF-8 task file at /tmp/task.md",
				"Kanban タスク task-1 を実行します; full instructions are in the UTF-8 task file at /tmp/task.md",
				"接管 Kanban 任务 task-1.",
				"Take over Kanban task task-1.",
				"Kanban タスク task-1 を引き継ぎます.",
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

func TestPromptLanguageDirectiveFromCardAndConfig(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en")

	paths := config.InstallPaths{
		Mode:     config.ModeGlobal,
		RulesDir: filepath.Join(t.TempDir(), "rules"),
	}

	makeAll := func(card string) []struct {
		name string
		body string
		err  error
	} {
		start, startErr := startAgentPrompt("task-1", paths, "", card)
		resume, resumeErr := resumeAgentPrompt("task-1", "go", paths, "working", card)
		notify, notifyErr := resumePrompt("task-1", "go", paths, "review", card)
		takeover, takeoverErr := takeoverAgentPrompt("task-1", "go", paths, "codex", "working", card)
		return []struct {
			name string
			body string
			err  error
		}{
			{"start", start, startErr},
			{"resume", resume, resumeErr},
			{"notify-resume", notify, notifyErr},
			{"takeover", takeover, takeoverErr},
		}
	}

	t.Run("card LANGUAGE", func(t *testing.T) {
		want := `in "ja"`
		for _, tc := range makeAll("- LANGUAGE: ja\n") {
			if tc.err != nil {
				t.Fatalf("%s: %v", tc.name, tc.err)
			}
			if !strings.Contains(tc.body, want) {
				t.Fatalf("%s missing %q: %s", tc.name, want, tc.body)
			}
			if !strings.Contains(tc.body, promptLanguageDirective("ja")) {
				t.Fatalf("%s missing full language directive: %s", tc.name, tc.body)
			}
		}
	})

	t.Run("fallback to agent_language", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		t.Setenv(config.EnvConfig, path)
		payload := `{"schema_version":1,"welcome_complete":true,"kanban_agent":"codex","launcher":"tmux","language":"en","agent_language":"zh-CN","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		want := `in "zh-CN"`
		for _, tc := range makeAll("- TYPE: Feature\n") {
			if tc.err != nil {
				t.Fatalf("%s: %v", tc.name, tc.err)
			}
			if !strings.Contains(tc.body, want) {
				t.Fatalf("%s missing %q: %s", tc.name, want, tc.body)
			}
		}
	})

	t.Run("missing config without LANGUAGE", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		t.Setenv(config.EnvConfig, path)
		for _, tc := range makeAll("- TYPE: Feature\n") {
			if tc.err == nil {
				t.Fatalf("%s: expected config error, got prompt %q", tc.name, tc.body)
			}
		}
		for _, tc := range makeAll("- LANGUAGE: ko\n") {
			if tc.err != nil {
				t.Fatalf("%s: card LANGUAGE should skip config: %v", tc.name, tc.err)
			}
			if !strings.Contains(tc.body, `in "ko"`) {
				t.Fatalf("%s missing ko directive: %s", tc.name, tc.body)
			}
		}
	})

	t.Run("corrupt config without LANGUAGE", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		t.Setenv(config.EnvConfig, path)
		if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range makeAll("- TYPE: Feature\n") {
			if tc.err == nil {
				t.Fatalf("%s: expected config error, got prompt %q", tc.name, tc.body)
			}
		}
		// Card LANGUAGE still wins even when config is unreadable.
		for _, tc := range makeAll("- LANGUAGE: ko\n") {
			if tc.err != nil {
				t.Fatalf("%s: card LANGUAGE should skip config: %v", tc.name, tc.err)
			}
			if !strings.Contains(tc.body, `in "ko"`) {
				t.Fatalf("%s missing ko directive: %s", tc.name, tc.body)
			}
		}
	})
}

func TestPromptUsesSizeIndependentOfDirectoryForm(t *testing.T) {
	for _, size := range []string{"small", "large"} {
		text := "- LANGUAGE: en\n- SIZE: " + size + "\n"
		instruction, err := RuleLoadingWithLanguage(config.InstallPaths{Mode: config.ModeGlobal, RulesDir: "/rules"}, text)
		if err != nil || !strings.Contains(instruction, config.Text("launch.prompt.size", size)) {
			t.Fatalf("size %s: %q %v", size, instruction, err)
		}
	}
}
