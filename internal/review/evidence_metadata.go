package review

import (
	"errors"
	"fmt"
	"iter"
	"strconv"
	"strings"
)

type evidenceMetadata struct {
	path, oldPath, status, before, after, numstat string
}

func evidenceDiffArgs(args ...string) []string {
	return append([]string{
		"-c", "core.quotePath=true", "diff", "--no-ext-diff", "--no-textconv", "--find-renames",
		"--submodule=short", "--src-prefix=a/", "--dst-prefix=b/",
	}, args...)
}

func splitEvidenceBlocks(text string) (prefix string, blocks []string) {
	if strings.HasPrefix(text, "\x1b[") || strings.Contains(text, "\n\x1b[") {
		// A forced Git color configuration may put SGR escapes around the
		// headers. Detect those blocks without changing their original bytes.
		start, offset := -1, 0
		for line := range strings.SplitAfterSeq(text, "\n") {
			if strings.HasPrefix(stripEvidenceHeaderColor(line), "diff --git ") {
				if start < 0 {
					prefix = text[:offset]
				} else {
					blocks = append(blocks, text[start:offset])
				}
				start = offset
			}
			offset += len(line)
		}
		if start < 0 {
			return text, nil
		}
		return prefix, append(blocks, text[start:])
	}
	if !strings.HasPrefix(text, "diff --git ") {
		idx := strings.Index(text, "\ndiff --git ")
		if idx < 0 {
			return text, nil
		}
		prefix, text = text[:idx+1], text[idx+1:]
	}
	for text != "" {
		idx := strings.Index(text, "\ndiff --git ")
		if idx < 0 {
			blocks = append(blocks, text)
			break
		}
		blocks = append(blocks, text[:idx+1])
		text = text[idx+1:]
	}
	return prefix, blocks
}

func stripEvidenceHeaderColor(line string) string {
	for remaining, ok := strings.CutPrefix(line, "\x1b["); ok; remaining, ok = strings.CutPrefix(line, "\x1b[") {
		code, rest, found := strings.Cut(remaining, "m")
		if !found || strings.ContainsFunc(code, func(r rune) bool { return r != ';' && (r < '0' || r > '9') }) {
			break
		}
		line = rest
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\x1b[m")
}

func collectEvidencePatch(section, from, to, text, raw, numstat string) (patch *evidencePatch, warning string, blocks, entries int) {
	prefix, parts := splitEvidenceBlocks(text)
	patch = &evidencePatch{from: from, to: to, prefix: prefix}
	metadata, err := readEvidenceMetadata(raw, numstat)
	if err != nil {
		warning = err.Error()
	} else {
		patch.units, warning = matchEvidenceBlocks(patch, section, parts, metadata)
	}
	if warning != "" {
		warning = fmt.Sprintf("%s (%d blocks, %d entries)", warning, len(parts), len(metadata))
		patch.units = nil
		for i, block := range parts {
			newPath, oldPath, parsed := fallbackEvidencePath(block)
			unit := newEvidenceDiff(patch, section, parts[i:i+1], newPath, parsed)
			unit.fallbackOld = oldPath
			unit.unavailable = warning
			patch.units = append(patch.units, unit)
		}
	}
	return patch, warning, len(parts), len(metadata)
}

func newEvidenceDiff(patch *evidencePatch, section string, blocks []string, path string, parsed bool) evidenceDiff {
	unit := evidenceDiff{patch: patch, section: section, blocks: blocks, path: path, parsed: parsed}
	for _, block := range blocks {
		unit.fullBytes += len(block)
	}
	return unit
}

func readEvidenceMetadata(raw, numstat string) ([]evidenceMetadata, error) {
	var metadata []evidenceMetadata
	nextRaw, stopRaw := iter.Pull(nulFields(raw))
	defer stopRaw()
	for header, ok := nextRaw(); ok; header, ok = nextRaw() {
		fields := strings.Fields(header)
		if len(fields) != 5 || !strings.HasPrefix(header, ":") {
			return metadata, errors.New("invalid raw diff entry")
		}
		path, ok := nextRaw()
		if !ok || path == "" {
			return metadata, errors.New("missing raw diff path")
		}
		entry := evidenceMetadata{path: path, status: fields[4], before: fields[2], after: fields[3]}
		if entry.status[0] == 'R' || entry.status[0] == 'C' {
			entry.oldPath = path
			entry.path, ok = nextRaw()
			if !ok || entry.path == "" {
				return metadata, errors.New("missing raw destination path")
			}
		}
		metadata = append(metadata, entry)
	}
	if (raw != "" && !strings.HasSuffix(raw, "\x00")) || (numstat != "" && !strings.HasSuffix(numstat, "\x00")) {
		return metadata, errors.New("unterminated diff metadata")
	}

	nextStat, stopStat := iter.Pull(nulFields(numstat))
	defer stopStat()
	for i := range metadata {
		row, ok := nextStat()
		added, rest, tab := strings.Cut(row, "\t")
		deleted, path, secondTab := strings.Cut(rest, "\t")
		if !ok || !tab || !secondTab || !validNumstatCount(added) || !validNumstatCount(deleted) {
			return metadata, errors.New("invalid numstat entry")
		}
		entry := &metadata[i]
		var old string
		if path == "" {
			var okOld, okNew bool
			old, okOld = nextStat()
			path, okNew = nextStat()
			if !okOld || !okNew || old == "" || path == "" {
				return metadata, errors.New("missing numstat paths")
			}
		}
		if entry.path != path || entry.oldPath != old {
			return metadata, errors.New("raw and numstat paths differ")
		}
		entry.numstat = added + "\t" + deleted
	}
	if _, ok := nextStat(); ok {
		return metadata, errors.New("raw and numstat counts differ")
	}
	return metadata, nil
}

func nulFields(text string) iter.Seq[string] {
	// The trailing empty part is the delimiter, not a field.
	if body, ok := strings.CutSuffix(text, "\x00"); ok {
		return strings.SplitSeq(body, "\x00")
	}
	if text == "" {
		return func(func(string) bool) {}
	}
	return strings.SplitSeq(text, "\x00")
}

func validNumstatCount(text string) bool {
	if text == "-" {
		return true
	}
	_, err := strconv.ParseUint(text, 10, 64)
	return err == nil
}

func matchEvidenceBlocks(patch *evidencePatch, section string, blocks []string, metadata []evidenceMetadata) ([]evidenceDiff, string) {
	var units []evidenceDiff
	for i := range metadata {
		entry := &metadata[i]
		count := 1
		if entry.status == "T" {
			count = 2
		}
		if len(blocks) < count {
			return nil, "patch and metadata counts differ"
		}
		for _, block := range blocks[:count] {
			old := entry.path
			if entry.oldPath != "" {
				old = entry.oldPath
			}
			header, _, _ := strings.Cut(block, "\n")
			if !matchesEvidenceHeader(header, old, entry.path) {
				return nil, "patch and metadata paths differ"
			}
		}
		unit := newEvidenceDiff(patch, section, blocks[:count], entry.path, true)
		unit.metadata = entry
		units = append(units, unit)
		blocks = blocks[count:]
	}
	if len(blocks) != 0 {
		return nil, "patch and metadata counts differ"
	}
	return units, ""
}

func matchesEvidenceHeader(header, expectedOld, expectedNew string) bool {
	for old, newPath := range evidenceHeaderPairs(header) {
		if old == expectedOld && newPath == expectedNew {
			return true
		}
	}
	return false
}

func evidenceHeaderPairs(header string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		pair, ok := strings.CutPrefix(header, "diff --git ")
		if !ok {
			return
		}
		// Unquoted names can contain spaces, including a literal " b/".
		// Verify both sides of a candidate boundary rather than splitting on space.
		for i := range len(pair) {
			if pair[i] != ' ' || !(strings.HasPrefix(pair[i+1:], "b/") || strings.HasPrefix(pair[i+1:], `"b/`)) {
				continue
			}
			left, okLeft := prefixedEvidencePath(pair[:i], "a/")
			right, okRight := prefixedEvidencePath(pair[i+1:], "b/")
			if okLeft && okRight && !yield(left, right) {
				return
			}
		}
	}
}

func prefixedEvidencePath(text, prefix string) (string, bool) {
	path, ok := unquoteEvidencePath(text)
	if !ok {
		return "", false
	}
	path, ok = strings.CutPrefix(path, prefix)
	return path, ok && path != ""
}

func unquoteEvidencePath(text string) (string, bool) {
	if !strings.HasPrefix(text, `"`) {
		return text, text != "" && !strings.ContainsAny(text, "\"\t\r\n")
	}
	path, err := strconv.Unquote(text)
	return path, err == nil && path != ""
}

func fallbackEvidencePath(block string) (path, oldPath string, parsed bool) {
	header, rest, _ := strings.Cut(block, "\n")
	header = stripEvidenceHeaderColor(header)
	var renamed, source, added, removed string
	for line := range strings.SplitSeq(rest, "\n") {
		line = stripEvidenceHeaderColor(line)
		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch" {
			break
		}
		switch {
		case strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy to "):
			_, value, _ := strings.Cut(line, " to ")
			renamed, _ = unquoteEvidencePath(value)
		case strings.HasPrefix(line, "rename from "), strings.HasPrefix(line, "copy from "):
			_, value, _ := strings.Cut(line, " from ")
			source, _ = unquoteEvidencePath(value)
		case strings.HasPrefix(line, "+++ "):
			added = strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\t")
		case strings.HasPrefix(line, "--- "):
			removed = strings.TrimSuffix(strings.TrimPrefix(line, "--- "), "\t")
		}
	}
	if renamed != "" {
		return renamed, source, true
	}
	if path, ok := prefixedEvidencePath(added, "b/"); ok {
		return path, "", true
	}
	if added == "/dev/null" {
		if path, ok := prefixedEvidencePath(removed, "a/"); ok {
			return path, "", true
		}
	}

	var equalPath string
	for left, right := range evidenceHeaderPairs(header) {
		if left == right {
			if equalPath != "" {
				equalPath = ""
				break
			}
			equalPath = right
		}
	}
	if equalPath != "" {
		return equalPath, "", true
	}
	return strings.TrimPrefix(header, "diff --git "), "", false
}
