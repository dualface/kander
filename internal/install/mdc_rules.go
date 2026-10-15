package install

import (
	"path/filepath"
	"strings"
)

// alwaysApplyFrontmatter makes a cursor .mdc rule apply to every session; a
// rule without it is only attached on demand and would not load the entry.
const alwaysApplyFrontmatter = "---\nalwaysApply: true\n---\n\n"

// mdcFrontmatter classifies the leading frontmatter of a .mdc rules file.
type mdcFrontmatter int

const (
	mdcNoFrontmatter mdcFrontmatter = iota
	mdcAlwaysApply
	// mdcOtherFrontmatter is a frontmatter block without alwaysApply: true, or
	// one that is never closed; it belongs to the user and is not rewritten.
	mdcOtherFrontmatter
)

func isMDCTarget(target string) bool {
	return strings.EqualFold(filepath.Ext(target), ".mdc")
}

func classifyMDCFrontmatter(text string) mdcFrontmatter {
	lines := strings.Split(strings.TrimPrefix(text, "\uFEFF"), "\n")
	if strings.TrimSuffix(lines[0], "\r") != "---" {
		return mdcNoFrontmatter
	}
	always := false
	for _, line := range lines[1:] {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "---" {
			if always {
				return mdcAlwaysApply
			}
			return mdcOtherFrontmatter
		}
		if key, value, ok := strings.Cut(line, ":"); ok &&
			strings.TrimSpace(key) == "alwaysApply" && strings.TrimSpace(value) == "true" {
			always = true
		}
	}
	return mdcOtherFrontmatter
}
