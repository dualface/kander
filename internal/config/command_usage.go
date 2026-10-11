package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/rules"
)

// commandContractsDir is the rules-root directory that holds one contract file per command.
const commandContractsDir = rules.SnippetsDir + "/commands"

// rulesRootPlaceholder stands in for the rules root when no installation can be resolved.
const rulesRootPlaceholder = "<rules root>"

// CommandReadSentence is the conditional read sentence every rule file, prompt, and usage
// output repeats verbatim, with rulesRoot in place of the rules root.
func CommandReadSentence(rulesRoot string) string {
	return "Before running any `kander` command in this session, read `" + rulesRoot + "/" + commandContractsDir +
		"/README.md` once. Before the first use of a command, read its contract file."
}

// CommandUsage returns the usage text of command: the syntax lines of its contract file, the
// contract path, and the conditional read sentence. The contract is read from the current
// scope's rules root, falling back to the embedded copy.
func CommandUsage(command string) string {
	rulesDir := ""
	if paths, err := CurrentInstallPaths(); err == nil {
		rulesDir = paths.RulesDir
	}
	return commandUsage(rulesDir, command, os.Stderr)
}

// commandUsage renders the usage text. When the installed contract is missing and the
// embedded copy stands in, the Contract line still names the installed path the agent will
// look for, so the embedded-fallback hint goes to warn to make the gap visible.
func commandUsage(rulesDir, command string, warn io.Writer) string {
	name := commandContractsDir + "/" + command + ".md"
	root := rulesRootPlaceholder
	if rulesDir != "" {
		root = process.AgentPath(filepath.Clean(rulesDir))
	} else {
		warn = nil
	}
	var lines []string
	if data, source, err := rules.Installed(rulesDir, name); err == nil {
		warnEmbeddedFallback(warn, name, source)
		lines = contractSyntaxLines(string(data))
	}
	if len(lines) == 0 {
		lines = []string{"kander " + command}
	}
	return strings.Join(lines, "\n") + "\nContract: " + root + "/" + name + "\n" + CommandReadSentence(root)
}

// contractSyntaxLines returns the lines of the first fenced code block under the "## Syntax"
// heading of a contract file.
func contractSyntaxLines(contract string) []string {
	var lines []string
	inSyntax, inBlock := false, false
	for _, line := range strings.Split(contract, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			if inBlock {
				return lines
			}
			inSyntax = strings.TrimSpace(line) == "## Syntax"
		case inSyntax && strings.HasPrefix(line, "```"):
			if inBlock {
				return lines
			}
			inBlock = true
		case inBlock:
			lines = append(lines, line)
		}
	}
	return lines
}
