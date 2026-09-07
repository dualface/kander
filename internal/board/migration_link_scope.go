package board

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Unsupported spellings need intervention only if their location or possible
// target changes. Check both URI and Windows spellings without editing either.
func unsupportedTargetMoves(raw, from, to string, mapping map[string]string, wiki bool) bool {
	if migrationPathKey(from) != migrationPathKey(to) {
		return true
	}
	destination := markdownURL([]byte(raw))
	if wiki {
		destination = strings.SplitN(destination, "|", 2)[0]
	}
	destination = strings.SplitN(strings.SplitN(destination, "#", 2)[0], "?", 2)[0]
	if destination == "" {
		return false
	}
	if parsed, err := url.Parse(destination); err == nil && (parsed.IsAbs() || parsed.Host != "") {
		return false
	}
	if decoded, err := url.PathUnescape(destination); err == nil {
		destination = decoded
	}
	destination = strings.ReplaceAll(destination, "\\", "/")
	target := filepath.Join(filepath.Dir(from), filepath.FromSlash(destination))
	if _, ok := mapping[migrationPathKey(target)]; ok {
		return true
	}
	if wiki {
		if _, ok := mapping[migrationPathKey(target+".md")]; ok {
			return true
		}
		if !strings.Contains(destination, "/") {
			for path := range mapping {
				if strings.EqualFold(strings.TrimSuffix(filepath.Base(path), ".md"), destination) {
					return true
				}
			}
		}
	}
	return false
}

// Srcset URLs end at whitespace; commas within data URLs are part of the URL.
// Descriptors end at a separating comma. We only decide whether a rewrite is
// necessary, never normalize or publish unsupported HTML syntax.
func srcsetNeedsRelocation(value, from, to string, mapping map[string]string) bool {
	for len(value) > 0 {
		value = strings.TrimLeft(value, " \t\r\n\f,")
		if value == "" {
			break
		}
		end := strings.IndexAny(value, " \t\r\n\f")
		if end < 0 {
			end = len(value)
		}
		candidate := value[:end]
		value = value[end:]
		trailingComma := strings.HasSuffix(candidate, ",")
		candidate = strings.TrimRight(candidate, ",")
		// Adjacent non-data candidates may be written without whitespace.
		candidates := []string{candidate}
		if !strings.HasPrefix(strings.ToLower(candidate), "data:") {
			candidates = strings.Split(candidate, ",")
		}
		for _, raw := range candidates {
			next, err := relocateURL(raw, from, to, mapping)
			if err != nil || next != raw {
				return true
			}
		}
		if trailingComma {
			continue
		}
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			value = value[comma+1:]
		} else {
			break
		}
	}
	return false
}
