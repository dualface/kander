package config

import "testing"

func TestCatalogTextFollowsLanguageChanges(t *testing.T) {
	setupHome(t)
	t.Cleanup(resetLanguageState)
	t.Setenv(EnvLang, "en_US.UTF-8")
	if got := Text("cli.commands"); got != "commands:" {
		t.Fatalf("environment: %q", got)
	}
	BindConfigLanguage(&Config{Language: "cn"})
	if got := Text("cli.commands"); got != "子命令:" {
		t.Fatalf("config: %q", got)
	}
	ApplyLanguageArgument([]string{"--lang", "en"})
	if got := Text("cli.commands"); got != "commands:" {
		t.Fatalf("CLI: %q", got)
	}
	ApplyLanguageArgument(nil)
	if got := Text("cli.commands"); got != "子命令:" {
		t.Fatalf("cleared CLI: %q", got)
	}
	BindConfigLanguage(&Config{Language: "en"})
	if got := Text("cli.commands"); got != "commands:" {
		t.Fatalf("changed config: %q", got)
	}
}

func TestResolveScopeLanguageIgnoresBoundConfig(t *testing.T) {
	setupHome(t)
	t.Cleanup(resetLanguageState)
	ApplyLanguageArgument(nil)
	t.Setenv(EnvLangCLI, "")
	t.Setenv(EnvLang, "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	BindConfigLanguage(&Config{Language: "ja"})
	if got := ResolveLanguage(); got != "ja" {
		t.Fatalf("ResolveLanguage should keep the bound value, got %q", got)
	}
	if got := ResolveScopeLanguage(); got != "en" {
		t.Fatalf("ResolveScopeLanguage should ignore the bound value, got %q", got)
	}
}

func TestResolveLanguageJapaneseLocale(t *testing.T) {
	setupHome(t)
	t.Cleanup(resetLanguageState)
	ApplyLanguageArgument(nil)
	BindConfigLanguage(nil)
	t.Setenv(EnvLangCLI, "")
	t.Setenv(EnvLang, "ja_JP.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	if got := ResolveLanguage(); got != "ja" {
		t.Fatalf("ja_JP locale should resolve to ja, got %q", got)
	}
}

func TestJapaneseConfigAndMenuLabels(t *testing.T) {
	setupHome(t)
	t.Cleanup(resetLanguageState)
	ApplyLanguageArgument([]string{"--lang", "ja"})
	want := map[string]string{
		"config.kanban_agent":               "かんばん Agent",
		"config.kanban_model":               "かんばんモデル",
		"config.large":                      "大規模",
		"config.launcher":                   "起動方式",
		"config.small":                      "小規模",
		"menu.launcher":                     "起動方式",
		"menu.model_2":                      "codex モデル",
		"menu.effort_2":                     "codex 推論強度",
		"menu.large_task":                   "大規模タスク",
		"menu.review":                       "レビュー",
		"menu.models_and_reasoning_efforts": "モデルと推論強度",
		"menu.reasoning_effort_for":         "codex の推論強度は?",
		"menu.reasoning_effort_for_s":       "codexの推論強度は?",
		"config.languageLabels.ja":          "日本語",
	}
	for id, expected := range want {
		got := Text(id)
		switch id {
		case "menu.model_2", "menu.effort_2", "menu.reasoning_effort_for", "menu.reasoning_effort_for_s":
			got = Text(id, "codex")
		}
		if got != expected {
			t.Fatalf("%s = %q, want %q", id, got, expected)
		}
	}
	if FormatLanguageSummary("ja") != "日本語" {
		t.Fatalf("FormatLanguageSummary(ja)=%q", FormatLanguageSummary("ja"))
	}
}
