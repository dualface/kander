package terminal

import "strings"

// powerShellQuotes doubles every character PowerShell reads as a single quote:
// U+0027 and the typographic U+2018, U+2019, U+201A and U+201B.
var powerShellQuotes = strings.NewReplacer(
	"'", "''",
	"\u2018", "\u2018\u2018",
	"\u2019", "\u2019\u2019",
	"\u201a", "\u201a\u201a",
	"\u201b", "\u201b\u201b",
)

// PowerShellQuote renders value as a single-quoted PowerShell string, which is
// literal. It always quotes. It is the only PowerShell quoting of Kander.
func PowerShellQuote(value string) string {
	return "'" + powerShellQuotes.Replace(value) + "'"
}
