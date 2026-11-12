package rules

import "strings"

// CommandSyntaxLines includes additional v2 command forms without rewriting
// the manually maintained README command index. Both forms drive CLI help.
func CommandSyntaxLines(contract string) []string {
	var lines []string
	inSyntax, inBlock := false, false
	for line := range strings.SplitSeq(contract, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			inBlock = false
			inSyntax = line == "## Syntax" || line == "## Additional Syntax"
		case inSyntax && strings.HasPrefix(line, "```"):
			inBlock = !inBlock
		case inSyntax && inBlock:
			lines = append(lines, line)
		}
	}
	return lines
}
