package install

import "strings"

// rulesReferenceBlockHeading is the Markdown heading the markdown reference writes; a section
// under it is a Kander load-command block.
const rulesReferenceBlockHeading = "## Kander Rules Entry"

// markdownReference asks the agent in prose to read the entry at the start of a session. A
// rules file that already embeds the entry's content counts as referencing it.
type markdownReference struct{}

func (markdownReference) Name() string { return "markdown-reference" }

func (markdownReference) Block(spelling string) string {
	return rulesReferenceBlockHeading + "\n\n" +
		"At the start of every session, read `" + spelling +
		"` and follow it as the Kander workflow rules entry.\n"
}

func (markdownReference) Present(text, entry, baseDir string) bool {
	return mergedRulesPresent(text, entry) || entryReferencePresent(text, entry, baseDir)
}

// LoadCommand accepts a bare entry path line, optionally wrapped in backticks.
func (markdownReference) LoadCommand(trimmed string) (string, bool) {
	bare := trimmed
	if len(bare) > 1 && strings.HasPrefix(bare, "`") && strings.HasSuffix(bare, "`") {
		bare = strings.TrimSpace(bare[1 : len(bare)-1])
	}
	return bare, isEntryPathToken(bare)
}

func (markdownReference) ProjectWriteFirst() bool { return false }

func (markdownReference) ProjectSymlinkMayDiffer() bool { return false }
