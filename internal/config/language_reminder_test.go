package config

import (
	"strings"
	"testing"
)

func TestLanguageReminderFixedChoices(t *testing.T) {
	for value, want := range map[string]string{
		"en":    "Communicate in the en language.",
		"zh-CN": "用 zh-CN 语言交流。",
		"zh-TW": "用 zh-TW 語言交流。",
		"ja":    "ja 言語でやり取りしてください。",
		"ko":    "ko 언어로 소통하세요.",
		"es":    "Comunícate en el idioma es.",
		"fr":    "Communique dans la langue fr.",
		"de":    "Kommuniziere in der Sprache de.",
		" ja ":  "ja 言語でやり取りしてください。",
	} {
		if got := LanguageReminder(value); got != want {
			t.Errorf("LanguageReminder(%q)=%q, want %q", value, got, want)
		}
	}
}

func TestLanguageReminderSubtagFallback(t *testing.T) {
	simplified, traditional := "用 %s 语言交流。", "用 %s 語言交流。"
	for value, template := range map[string]string{
		"ZH-cn":      simplified,
		"zh":         simplified,
		"zh-Hans-TW": simplified,
		"zh-Hant-TW": traditional,
		"zh-HK":      traditional,
		"zh_TW":      traditional,
		"ja-JP":      "%s 言語でやり取りしてください。",
		"fr-CA":      "Communique dans la langue %s.",
		"EN-us":      "Communicate in the %s language.",
		"pt-BR":      "Communicate in the %s language.",
	} {
		if got, want := LanguageReminder(value), strings.Replace(template, "%s", value, 1); got != want {
			t.Errorf("LanguageReminder(%q)=%q, want %q", value, got, want)
		}
	}
}

func TestLanguageReminderRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "   ", "ja\r", "ja\x1b[0m", strings.Repeat("a", 65)} {
		if got := LanguageReminder(value); got != "" {
			t.Errorf("LanguageReminder(%q)=%q, want empty", value, got)
		}
		if got := WithLanguageReminder("line", value); got != "line" {
			t.Errorf("WithLanguageReminder(%q)=%q, want the line unchanged", value, got)
		}
	}
	if got := WithLanguageReminder("line", "de"); got != "line Kommuniziere in der Sprache de." {
		t.Fatalf("WithLanguageReminder=%q", got)
	}
}
