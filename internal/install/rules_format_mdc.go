package install

import "strings"

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

// mdcRule is a cursor rule file. It must carry alwaysApply frontmatter to load in every
// session, so Kander adds the frontmatter to a file without one and never rewrites a
// frontmatter block the user owns. It is never read or written through a symlink.
type mdcRule struct{}

func (mdcRule) Name() string { return "mdc-rule" }

func (mdcRule) SymlinkRefusal() string { return "menu.mdc_rule_is_symlink" }

func (mdcRule) Inspect(text string) (formatState, string) {
	switch classifyMDCFrontmatter(text) {
	case mdcNoFrontmatter:
		return formatMissingPrefix, "menu.mdc_rule_missing_frontmatter"
	case mdcOtherFrontmatter:
		return formatForeign, "menu.mdc_rule_not_always_applied"
	}
	return formatReady, ""
}

func (mdcRule) Prefix() string { return alwaysApplyFrontmatter }

// OnlyKander is true for the frontmatter Kander wrote and nothing else.
func (mdcRule) OnlyKander(rest string) bool {
	return rest == strings.TrimSpace(alwaysApplyFrontmatter)
}
