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
