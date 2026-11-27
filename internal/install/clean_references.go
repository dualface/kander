package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rulesEntryTokenRE extracts a path token ending in KANDER-AGENTS.md. The character class is
// the same set isPathChar accepts, so a token never swallows surrounding prose.
var rulesEntryTokenRE = regexp.MustCompile(`[A-Za-z0-9._\-/\\~:]*KANDER-AGENTS\.md`)

// refClass is the classification of one load-command line.
type refClass int

const (
	// refInvalid drops the line: the path does not resolve to the current entry.
	refInvalid refClass = iota
	// refDuplicate drops the line: an earlier valid load command already stays.
	refDuplicate
	// refValid keeps the line and claims the single valid slot.
	refValid
)

// CleanRulesReferences removes invalid and duplicate Kander rules load commands from text and
// returns the result with the number of dropped lines. A load command is invalid when its path
// does not resolve to entry, such as the retired .kander/rules/KANDER-AGENTS.md of a project
// install. Load commands are only the forms Kander writes or equivalents: a whole-line
// `@<path>` claude-import, a `## Kander Rules Entry` block, and a whole-line bare path, each
// naming a KANDER-AGENTS.md path. Lines inside HTML comments and code fences, and prose
// mentioning the path among other words, are never touched.
func CleanRulesReferences(text, entry, baseDir string) (string, int) {
	removed := 0
	spellings := entrySpellings(entry, baseDir)
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
		return refInvalid
	}
	apply := func(i int, class refClass) {
		if class != refValid {
			drop[i] = true
			removed++
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
				if class == refValid {
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
	if removed == 0 {
		return text, 0
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
	return out.String(), removed
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
	dead := htmlCommentMask(text)
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

// htmlCommentMask marks every byte of text that lies inside an HTML comment, an unclosed
// comment running to the end.
func htmlCommentMask(text string) []bool {
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
	return dead
}

// RemoveRulesReferences removes every Kander rules load command from text, whatever entry it
// names, and returns the result with the number of dropped load commands. It recognizes the
// same forms as CleanRulesReferences and also drops the `## Kander Rules Entry` heading and the
// blank lines of a block that held nothing else, plus the blank line Kander put before an
// appended block. Lines inside HTML comments and code fences stay, and comment text sharing a
// line with a load command is kept on that line.
func RemoveRulesReferences(text string) (string, int) {
	trailingNewline := strings.HasSuffix(text, "\n")
	body := strings.TrimSuffix(text, "\n")
	lines := strings.Split(body, "\n")
	live := liveLines(body)
	comments := commentLines(body)
	drop := make([]bool, len(lines))
	removed := 0
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
			tokens, other := 0, 0
			for j := i + 1; j < end; j++ {
				if _, ok := firstEntryToken(live[j]); ok {
					drop[j] = true
					tokens++
				} else if strings.TrimSpace(lines[j]) != "" {
					other++
				}
			}
			if tokens > 0 {
				removed += tokens
				drop[i] = true
				if other == 0 {
					for j := i + 1; j < end; j++ {
						drop[j] = true
					}
				}
			}
			i = end - 1
			continue
		}
		if _, ok := loadCommandToken(trimmed); ok {
			drop[i] = true
			removed++
		}
	}
	if removed == 0 {
		return text, 0
	}
	// An appended block was separated from the content above by one blank line; drop it when
	// the removed lines ran to the end of the file or up to another blank line.
	for i := 1; i < len(lines); i++ {
		if !drop[i] || drop[i-1] || strings.TrimSpace(lines[i-1]) != "" {
			continue
		}
		j := i
		for j < len(lines) && drop[j] {
			j++
		}
		if j == len(lines) || strings.TrimSpace(lines[j]) == "" {
			drop[i-1] = true
		}
	}
	var kept []string
	for i, line := range lines {
		switch {
		case !drop[i]:
			kept = append(kept, line)
		case comments[i] != "":
			// The load command shares its line with comment text the user wrote; only the
			// command goes.
			kept = append(kept, comments[i])
		}
	}
	out := strings.Join(kept, "\n")
	if trailingNewline && len(kept) > 0 {
		out += "\n"
	}
	return out, removed
}

// commentLines returns, per line, the HTML comment text the line holds, trimmed; a line without
// a comment yields "".
func commentLines(text string) []string {
	dead := htmlCommentMask(text)
	lines := strings.Split(text, "\n")
	out := make([]string, len(lines))
	pos := 0
	for i, line := range lines {
		var b strings.Builder
		for j := 0; j < len(line); j++ {
			if dead[pos+j] {
				b.WriteByte(line[j])
			}
		}
		out[i] = strings.TrimSpace(b.String())
		pos += len(line) + 1
	}
	return out
}
