package terminal

import (
	"slices"
	"strings"
)

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

// forwardedEnv are the caller's variables a Windows pane shell receives: a
// terminal service passes none of the caller's environment.
var forwardedEnv = []string{"KANBAN_DIR", "KANDER_CONFIG", "KANDER_LANG"}

// ForwardedEnv lists the variables powershell_env forwards, so a launch can
// check their values before it creates a container.
func ForwardedEnv() []string { return slices.Clone(forwardedEnv) }

// powerShellEnv renders the statements that make the forwarded variables equal
// to the caller's. An empty or unset one is removed, because the terminal
// service may supply a stale value from the registry or its own environment.
func powerShellEnv(getenv func(string) string) string {
	var statements strings.Builder
	for _, name := range forwardedEnv {
		value := "$null"
		if current := getenv(name); current != "" {
			value = PowerShellQuote(current)
		}
		statements.WriteString("$env:" + name + "=" + value + "; ")
	}
	return statements.String()
}
