package config

import "strings"

// languageReminders holds the reminder sentence for each supported primary language, written in
// that language itself; %s is the agent language value. The released prompt templates are
// English only, so these localized sentences live here instead.
var languageReminders = map[string]string{
	"en":      "Communicate in the %s language.",
	"zh-hans": "用 %s 语言交流。",
	"zh-hant": "用 %s 語言交流。",
	"ja":      "%s 言語でやり取りしてください。",
	"ko":      "%s 언어로 소통하세요.",
	"es":      "Comunícate en el idioma %s.",
	"fr":      "Communique dans la langue %s.",
	"de":      "Kommuniziere in der Sprache %s.",
}

// traditionalChineseRegions are the region subtags whose Chinese defaults to the traditional script.
var traditionalChineseRegions = map[string]bool{"tw": true, "hk": true, "mo": true}

// LanguageReminder returns the one-sentence reminder, in the language itself, to communicate in
// the agent language value. A value that fails the agent_language rule yields an empty string, so
// a hand-edited card LANGUAGE carrying control characters never reaches a delivered line.
func LanguageReminder(value string) string {
	language, err := ValidateAgentLanguage(value)
	if err != nil {
		return ""
	}
	return strings.Replace(languageReminders[reminderKey(language)], "%s", language, 1)
}

// WithLanguageReminder appends the reminder for language to a delivered one-line instruction,
// separated by one space; the line is returned unchanged when there is no reminder.
func WithLanguageReminder(line, language string) string {
	if reminder := LanguageReminder(language); reminder != "" {
		return line + " " + reminder
	}
	return line
}

// reminderKey maps a language tag to a languageReminders key. Subtags split on "-" or "_" and
// compare case-insensitively; after the primary subtag, a 4-letter subtag is the script and a
// 2-letter or 3-digit subtag is the region. Unknown primary languages fall back to English.
func reminderKey(language string) string {
	parts := strings.FieldsFunc(strings.ToLower(language), func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return "en"
	}
	primary := parts[0]
	if primary != "zh" {
		if _, ok := languageReminders[primary]; ok {
			return primary
		}
		return "en"
	}
	script, region := "", ""
	for _, part := range parts[1:] {
		switch {
		case script == "" && len(part) == 4 && isLetters(part):
			script = part
		case region == "" && (len(part) == 2 && isLetters(part) || len(part) == 3 && isDigits(part)):
			region = part
		}
	}
	if script == "hant" || script != "hans" && traditionalChineseRegions[region] {
		return "zh-hant"
	}
	return "zh-hans"
}

func isLetters(text string) bool {
	for _, r := range text {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func isDigits(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
