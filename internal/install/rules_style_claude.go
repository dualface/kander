package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var claudeImportRE = regexp.MustCompile(`^@(\S+)$`)

// claudeImport references the entry with an `@<path>` import line, which the agent expands
// itself.
type claudeImport struct{}

func (claudeImport) Name() string { return "claude-import" }

func (claudeImport) Block(spelling string) string { return "@" + spelling + "\n" }

func (claudeImport) Present(text, entry, baseDir string) bool {
	candidates := ruleEntryCandidates(entry)
	body := stripCommentsAndFences(text)
	offset := 0
	for _, raw := range splitKeepEnds(body) {
		line := strings.TrimSpace(raw)
		lineStart := offset
		offset += len(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := claudeImportRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		ref := strings.TrimSpace(match[1])
		accepted := false
		if _, ok := candidates[ref]; ok {
			accepted = true
		}
		if !accepted {
			expanded := ref
			if strings.HasPrefix(ref, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					expanded = home + ref[1:]
				}
			}
			if !filepath.IsAbs(expanded) {
				expanded = filepath.Join(baseDir, expanded)
			}
			accepted = sameResolved(expanded, entry)
		}
		if !accepted {
			continue
		}
		context := candidateContext(body, lineStart, offset, true)
		if negationRE.MatchString(context) {
			continue
		}
		return true
	}
	return false
}

// LoadCommand accepts a whole `@<path>` line.
func (claudeImport) LoadCommand(trimmed string) (string, bool) {
	match := claudeImportRE.FindStringSubmatch(trimmed)
	if match == nil {
		return "", false
	}
	return match[1], isEntryPathToken(match[1])
}

func (claudeImport) ProjectWriteFirst() bool { return true }

func (claudeImport) ProjectSymlinkMayDiffer() bool { return true }
