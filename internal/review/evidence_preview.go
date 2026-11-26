package review

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func previewEvidenceDiff(blocks []string) string {
	var out strings.Builder
	var remainingBytes, remainingHunks int
	for _, block := range blocks {
		_, body, _ := strings.Cut(block, "\n")
		remainingBytes += len(body)
		remainingHunks += evidenceHunks(body)
	}

	// Reserve the largest possible remainder note before emitting complete
	// preview lines. Its actual counts only decrease as lines are kept.
	budget := evidenceUnitPreview - len(evidencePreviewRemainder(remainingHunks, remainingBytes))
	for _, block := range blocks {
		_, body, _ := strings.Cut(block, "\n")
		var minus, plus string
		inHunk := false
		for body != "" {
			line, rest, newline := strings.Cut(body, "\n")
			hunk := strings.HasPrefix(line, "@@ ")
			if hunk {
				end := strings.Index(rest, "\n@@ ")
				hunkBody := rest
				if end >= 0 {
					hunkBody = rest[:end+1]
				}
				minus, plus = hunkBody, hunkBody
				inHunk = true
			}

			start := 0
			if inHunk && len(line) != 0 {
				var paired string
				switch line[0] {
				case '-':
					paired, plus = nextPreviewLine(plus, '+')
				case '+':
					paired, minus = nextPreviewLine(minus, '-')
				}
				if len(line) >= evidencePreviewLine {
					start = previewStart(line, paired)
				}
			}

			preview := truncateEvidenceLine(line, start)
			needed := len(preview)
			if newline {
				needed++
			}
			if out.Len()+needed > budget {
				out.WriteString(evidencePreviewRemainder(remainingHunks, remainingBytes))
				return out.String()
			}
			out.WriteString(preview)
			if newline {
				out.WriteByte('\n')
			}
			body = rest
			remainingBytes -= len(line)
			if newline {
				remainingBytes--
			}
			if hunk {
				remainingHunks--
			}
		}
	}
	return out.String()
}

func evidenceHunks(body string) int {
	count := strings.Count(body, "\n@@ ")
	if strings.HasPrefix(body, "@@ ") {
		count++
	}
	return count
}

func evidencePreviewRemainder(hunks, size int) string {
	return fmt.Sprintf("[Preview limit: %d further hunk header(s), %d original bytes not shown.]\n", hunks, size)
}

func nextPreviewLine(body string, prefix byte) (string, string) {
	for body != "" {
		line, rest, _ := strings.Cut(body, "\n")
		if len(line) != 0 && line[0] == prefix {
			return line, rest
		}
		body = rest
	}
	return "", ""
}

func previewStart(line, paired string) int {
	if paired == "" {
		return 0
	}
	line, paired = line[1:], paired[1:]
	i := 0
	for i < min(len(line), len(paired)) && line[i] == paired[i] {
		i++
	}
	if i == len(line) && i == len(paired) {
		return 0
	}
	return max(0, i-evidencePreviewContext)
}

func truncateEvidenceLine(line string, start int) string {
	// Include a possible newline in the cap for the serialized preview.
	const lineLimit = evidencePreviewLine - 1
	if len(line) <= lineLimit {
		return line
	}
	validUTF8 := utf8.ValidString(line)
	var leading string
	if start > 0 {
		start++ // The change marker is never skipped.
		if validUTF8 {
			start = previousRuneBoundary(line, start)
		}
		leading = fmt.Sprintf("%c[%d leading bytes skipped] ", line[0], start-1)
	}
	if len(leading)+len(line)-start <= lineLimit {
		return leading + line[start:]
	}
	reserve := len(evidenceTrailingNote(len(line)))
	end := start + lineLimit - len(leading) - reserve
	if validUTF8 {
		end = previousRuneBoundary(line, end)
	}
	return leading + line[start:end] + evidenceTrailingNote(len(line)-end)
}

func evidenceTrailingNote(size int) string {
	return fmt.Sprintf(" [%d trailing bytes skipped]", size)
}

func previousRuneBoundary(line string, index int) int {
	for index > 0 && index < len(line) && !utf8.RuneStart(line[index]) {
		index--
	}
	return index
}
