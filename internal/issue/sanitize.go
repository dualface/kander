package issue

import (
	"regexp"
	"strings"
	"unicode"
)

// sanitizeMaxLength bounds every diagnostic that reaches the user.
const sanitizeMaxLength = 300

var (
	ansiPattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	// Token shapes recognized for defense in depth; credentials must never
	// reach a diagnostic even when a tool meant to mask them does not.
	tokenPatterns = []*regexp.Regexp{
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),
		regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
		regexp.MustCompile(`(?i)(token|password|passwd|secret|authorization)(\s*[:=]\s*)\S+`),
	}
)

// Sanitize turns arbitrary tool output into one bounded diagnostic line: it
// drops ANSI escapes and control characters, collapses whitespace, redacts
// token-shaped strings, and truncates the result.
func Sanitize(text string) string {
	cleaned := ansiPattern.ReplaceAllString(text, "")
	var builder strings.Builder
	for _, char := range cleaned {
		switch {
		case char == '\n' || char == '\r' || char == '\t':
			builder.WriteByte(' ')
		case unicode.IsPrint(char):
			builder.WriteRune(char)
		}
	}
	collapsed := strings.Join(strings.Fields(builder.String()), " ")
	redacted := redact(collapsed)
	if len(redacted) > sanitizeMaxLength {
		redacted = redacted[:sanitizeMaxLength] + "..."
	}
	return redacted
}

// redact replaces token-shaped substrings with a placeholder.
func redact(text string) string {
	redacted := text
	for _, pattern := range tokenPatterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}
