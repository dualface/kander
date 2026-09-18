package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// rulesReferenceBlockHeading is the Markdown heading EnsureRulesIntegration writes for
// markdown-reference agents; a section under it is a Kander load-command block.
const rulesReferenceBlockHeading = "## Kander Rules Entry"

// rulesEntryTokenRE extracts a path token ending in KANDER-AGENTS.md. The character class is
// the same set isPathChar accepts, so a token never swallows surrounding prose.
var rulesEntryTokenRE = regexp.MustCompile(`[A-Za-z0-9._\-/\\~:]*KANDER-AGENTS\.md`)

var claudeImportRE = regexp.MustCompile(`^@(\S+)$`)

// RulesReferenceIssues counts the load commands InspectRulesReferences found that the repair
// pipeline would drop from one agent rules file.
type RulesReferenceIssues struct {
	// Invalid counts load commands whose path does not resolve to the current rules entry.
	Invalid int
	// Duplicates counts load commands dropped because an earlier valid one stays.
	Duplicates int
}

// Total returns the number of load commands the repair pipeline would drop.
func (i RulesReferenceIssues) Total() int {
	return i.Invalid + i.Duplicates
}

// refClass is the classification of one load-command line.
type refClass int

const (
	// refInvalid drops the line: the path does not resolve to the current entry.
	refInvalid refClass = iota
	// refDuplicate drops the line: an earlier valid load command already stays.
	refDuplicate
	// refValid keeps the line and claims the single valid slot.
	refValid
	// refLegacy keeps the line untouched: it names the previous rules root, which the
	// rewrite pass owns; it never counts as invalid and never claims the valid slot.
	refLegacy
)

// InspectRulesReferences simulates the repair pipeline on the agent rules file — legacy
// rewrite followed by the reference cleanup — and counts what it would drop, without writing.
// A missing or unreadable file reports an error and zero issues.
func InspectRulesReferences(agent string, paths config.InstallPaths) (RulesReferenceIssues, error) {
	var issues RulesReferenceIssues
	target := AgentRulesTarget(agent, paths)
	if target == "" {
		return issues, nil
	}
	resolved := target
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if real, err := filepath.EvalSymlinks(target); err == nil {
			resolved = real
		}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return issues, err
	}
	baseDir := filepath.Dir(target)
	text := string(data)
	if rewritten, changed := rewriteLegacyReference(text, paths, baseDir); changed {
		text = rewritten
	}
	_, issues = cleanRulesReferences(text, paths, baseDir)
	return issues, nil
}

// CleanRulesReferences removes invalid and duplicate Kander rules load commands from text and
// returns the result with the number of dropped lines. Load commands are only the forms Kander
// writes or equivalents: a whole-line `@<path>` claude-import, a `## Kander Rules Entry`
// block, and a whole-line bare path, each naming a KANDER-AGENTS.md path. Lines inside HTML
// comments and code fences, and prose mentioning the path among other words, are never
// touched. Callers that want legacy references rewritten must run rewriteLegacyReference
// first; references to the previous rules root are otherwise preserved.
func CleanRulesReferences(text string, paths config.InstallPaths, baseDir string) (string, int) {
	cleaned, issues := cleanRulesReferences(text, paths, baseDir)
	return cleaned, issues.Total()
}

// cleanRulesReferences is the shared line-level cleaner behind CleanRulesReferences and
// InspectRulesReferences.
func cleanRulesReferences(text string, paths config.InstallPaths, baseDir string) (string, RulesReferenceIssues) {
	var issues RulesReferenceIssues
	entry := RulesEntry(paths)
	spellings := entrySpellings(entry, baseDir)
	var legacySpellings map[string]struct{}
	if legacy := legacyRulesEntry(paths); legacy != "" {
		legacySpellings = legacyEntrySpellings(legacy, baseDir)
	}
	lines := strings.Split(text, "\n")
	live := liveLines(text)
	drop := make([]bool, len(lines))
	validKept := false
	classify := func(token string) refClass {
		if referenceResolves(token, spellings, entry, baseDir) {
			if validKept {
				return refDuplicate
			}
			validKept = true
			return refValid
		}
		if _, ok := legacySpellings[token]; ok {
			return refLegacy
		}
		if _, ok := legacySpellings[filepath.ToSlash(token)]; ok {
			return refLegacy
		}
		return refInvalid
	}
	apply := func(i int, class refClass) {
		switch class {
		case refInvalid:
			drop[i] = true
			issues.Invalid++
		case refDuplicate:
			drop[i] = true
			issues.Duplicates++
		}
	}
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(live[i])
		if trimmed == "" {
			continue
		}
		if trimmed == rulesReferenceBlockHeading {
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(live[end]), "#") {
				end++
			}
			tokenLines := 0
			kept := 0
			for j := i + 1; j < end; j++ {
				token, ok := firstEntryToken(live[j])
				if !ok {
					continue
				}
				tokenLines++
				class := classify(token)
				apply(j, class)
				if class == refValid || class == refLegacy {
					kept++
				}
			}
			// The heading is part of the load-command block: once every load line in it is
			// gone, the leftover heading only announces an empty section.
			if tokenLines > 0 && kept == 0 {
				drop[i] = true
			}
			i = end - 1
			continue
		}
		token, ok := loadCommandToken(trimmed)
		if !ok {
			continue
		}
		apply(i, classify(token))
	}
	if issues.Total() == 0 {
		return text, issues
	}
	var out strings.Builder
	for i, line := range lines {
		if drop[i] {
			continue
		}
		out.WriteString(line)
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String(), issues
}

// loadCommandToken returns the Kander rules entry path of a whole-line load command: a
// claude-import `@<path>` line or a bare path line, optionally wrapped in backticks.
func loadCommandToken(trimmed string) (string, bool) {
	if match := claudeImportRE.FindStringSubmatch(trimmed); match != nil {
		return match[1], isEntryPathToken(match[1])
	}
	bare := trimmed
	if len(bare) > 1 && strings.HasPrefix(bare, "`") && strings.HasSuffix(bare, "`") {
		bare = strings.TrimSpace(bare[1 : len(bare)-1])
	}
	return bare, isEntryPathToken(bare)
}

// firstEntryToken extracts the first KANDER-AGENTS.md path token from a line's live text;
// it must carry a path separator or ~ so a bare filename mention never counts.
func firstEntryToken(live string) (string, bool) {
	token := rulesEntryTokenRE.FindString(live)
	return token, isEntryPathToken(token)
}

// isEntryPathToken reports whether s is a path reference to a KANDER-AGENTS.md file: the
// whole string is path characters, ends with the entry name, and names a location rather
// than the bare filename.
func isEntryPathToken(s string) bool {
	if s == "" || !strings.HasSuffix(s, "KANDER-AGENTS.md") {
		return false
	}
	if !strings.ContainsAny(s, "/\\~") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isPathChar(s[i]) {
			return false
		}
	}
	return true
}

// referenceResolves reports whether ref names the current rules entry, by exact accepted
// spelling or by resolving it on disk relative to the rules file directory.
func referenceResolves(ref string, spellings map[string]struct{}, entry, baseDir string) bool {
	if _, ok := spellings[ref]; ok {
		return true
	}
	if _, ok := spellings[filepath.ToSlash(ref)]; ok {
		return true
	}
	expanded := ref
	if strings.HasPrefix(ref, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = home + ref[1:]
		}
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(baseDir, expanded)
	}
	return sameResolved(expanded, entry)
}

// liveLines returns the per-line text with HTML comment spans and code-fence contents
// blanked out, mirroring stripCommentsAndFences so classification sees exactly the active
// lines while the original lines stay available for output.
func liveLines(text string) []string {
	dead := make([]bool, len(text))
	for i := 0; i < len(text); {
		rel := strings.Index(text[i:], "<!--")
		if rel < 0 {
			break
		}
		start := i + rel
		end := strings.Index(text[start+4:], "-->")
		if end < 0 {
			for j := start; j < len(text); j++ {
				dead[j] = true
			}
			break
		}
		stop := start + 4 + end + 3
		for j := start; j < stop; j++ {
			dead[j] = true
		}
		i = stop
	}
	lines := strings.Split(text, "\n")
	live := make([]string, len(lines))
	pos := 0
	for i, line := range lines {
		var b strings.Builder
		for j := 0; j < len(line); j++ {
			if pos+j < len(dead) && !dead[pos+j] {
				b.WriteByte(line[j])
			}
		}
		live[i] = b.String()
		pos += len(line) + 1
	}
	var fence byte
	inFence := false
	fenceLen := 0
	for i, line := range live {
		stripped := strings.TrimLeft(line, " \t")
		if !inFence {
			opened := false
			for _, marker := range []string{"```", "~~~"} {
				if strings.HasPrefix(stripped, marker) {
					fence = marker[0]
					inFence = true
					fenceLen = len(stripped) - len(strings.TrimLeft(stripped, string(marker[:1])))
					opened = true
					break
				}
			}
			if opened {
				live[i] = ""
			}
			continue
		}
		live[i] = ""
		if strings.HasPrefix(stripped, strings.Repeat(string(fence), 3)) {
			closing := len(stripped) - len(strings.TrimLeft(stripped, string(fence)))
			if closing >= fenceLen {
				inFence = false
				fenceLen = 0
			}
		}
	}
	return live
}
