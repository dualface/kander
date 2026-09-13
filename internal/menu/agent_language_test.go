package menu

import (
	"slices"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestSessionKeepsStoredAgentLanguage(t *testing.T) {
	existing := config.DefaultConfig()
	existing.WelcomeComplete = true
	existing.Language = "en"
	existing.AgentLanguage = "ja"
	session, err := NewSessionForTest(existing)
	if err != nil {
		t.Fatal(err)
	}
	if session.Config.AgentLanguage != "ja" {
		t.Fatalf("session dropped agent_language: %q", session.Config.AgentLanguage)
	}
	session.SetAgentLanguage("  ko ")
	if session.Config.AgentLanguage != "ko" {
		t.Fatalf("SetAgentLanguage should trim, got %q", session.Config.AgentLanguage)
	}
}

func TestAgentLanguageChoicesFollowInterfaceLanguageAndPreserveOutOfList(t *testing.T) {
	t.Setenv(config.EnvLangCLI, "")
	config.ApplyLanguageArgument(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	values := []string{"en", "zh-CN", "zh-TW", "ja", "ko", "es", "fr", "de"}
	labels := map[string][]string{
		"en": {"English", "Simplified Chinese", "Traditional Chinese", "Japanese", "Korean", "Spanish", "French", "German"},
		"cn": {"英语", "简体中文", "繁体中文", "日语", "韩语", "西班牙语", "法语", "德语"},
		"ja": {"英語", "簡体字中国語", "繁体字中国語", "日本語", "韓国語", "スペイン語", "フランス語", "ドイツ語"},
	}
	for _, language := range []string{"en", "cn", "ja"} {
		config.BindConfigLanguage(&config.Config{Language: language})
		want := make([]Choice, len(values))
		for i, value := range values {
			want[i] = Choice{Value: value, Label: labels[language][i]}
		}
		if got := agentLanguageChoices("zh-CN"); !slices.Equal(got, want) {
			t.Fatalf("%s choices=%+v want %+v", language, got, want)
		}

		existing := config.DefaultConfig()
		existing.WelcomeComplete = true
		existing.AgentLanguage = "pt"
		session, err := NewSessionForTest(existing)
		if err != nil {
			t.Fatal(err)
		}
		if got := session.AgentLanguageChoices(); !slices.Equal(got, append(want, Choice{Value: "pt", Label: "pt"})) {
			t.Fatalf("%s out-of-list choices=%+v", language, got)
		}
	}
}
