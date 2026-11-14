package config

import (
	"strings"

	"github.com/dualface/kander/internal/i18n"
)

// LanguageConstraint renders the persistent communication requirement from the
// card language, independently of the CLI locale. Other languages use English
// wording while retaining the exact requested language value.
func LanguageConstraint(value string) string {
	language, err := ValidateAgentLanguage(value)
	if err != nil {
		return ""
	}
	locale := "en"
	primary, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(language, "_", "-")), "-")
	switch primary {
	case "zh":
		locale = "cn"
	case "ja":
		locale = "ja"
	}
	return i18n.Text(locale, "agent_language.task_constraint", language)
}
