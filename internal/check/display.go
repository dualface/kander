package check

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func escapeBytes(raw []byte) string {
	if utf8.Valid(raw) {
		return escapeRunes(string(raw))
	}
	var b strings.Builder
	for _, c := range raw {
		if c >= 0x20 && c < 0x7f && c != '\\' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, `\x%02x`, c)
	}
	return b.String()
}

func escapeRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if shouldEscapeRune(r) {
				b.WriteString(escapeControlRune(r))
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func shouldEscapeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
}

func escapeControlRune(r rune) string {
	if r < 0x80 {
		return fmt.Sprintf(`\x%02x`, r)
	}
	if r <= 0xffff {
		return fmt.Sprintf(`\u%04x`, r)
	}
	return fmt.Sprintf(`\U%08x`, r)
}

func escapeText(s string) string {
	return escapeBytes([]byte(s))
}

func displayPath(path GitPath) string {
	return escapeBytes(path.Raw)
}

func displayOptionalPath(path *GitPath) string {
	if path == nil {
		return ""
	}
	return displayPath(*path)
}

func cleanMessage(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || shouldEscapeRune(r) {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 512 {
		out = out[:512]
	}
	return out
}

func humanDelivery(result DeliveryResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t("check.status", result.Check, result.Status))
	if result.Error != nil {
		fmt.Fprintf(&b, "%s\n", t("check.error", result.Error.Code, result.Error.Message))
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n", t("check.commit_base", result.BaseCommit))
	fmt.Fprintf(&b, "%s\n", t("check.commit_target", result.TargetCommit))
	fmt.Fprintf(&b, "%s\n", t("check.diff_check", result.DiffCheck.Status))
	for _, line := range result.DiffCheck.Diagnostics {
		fmt.Fprintf(&b, "  %s\n", escapeText(line))
	}
	if len(result.AddedOverLimit) > 0 {
		fmt.Fprintf(&b, "%s\n", t("check.added_over_limit"))
		for _, item := range result.AddedOverLimit {
			b.WriteString(humanCandidate(item))
		}
	}
	if len(result.CrossedLimit) > 0 {
		fmt.Fprintf(&b, "%s\n", t("check.crossed_limit"))
		for _, item := range result.CrossedLimit {
			b.WriteString(humanCandidate(item))
		}
	}
	return b.String()
}

func humanOverlap(result OverlapResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t("check.status", result.Check, result.Status))
	if result.Error != nil {
		fmt.Fprintf(&b, "%s\n", t("check.error", result.Error.Code, result.Error.Message))
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n", t("check.merge_base", result.MergeBase))
	fmt.Fprintf(&b, "%s\n", t("check.commit_head", result.HeadCommit))
	fmt.Fprintf(&b, "%s\n", t("check.commit_source", result.SourceCommit))
	if len(result.Paths) > 0 {
		fmt.Fprintf(&b, "%s\n", t("check.overlap_paths"))
		for _, path := range result.Paths {
			fmt.Fprintf(&b, "  %s\n", displayPath(path))
		}
	}
	return b.String()
}

func humanCandidate(item LineCandidate) string {
	path := displayPath(item.Path)
	if item.BasePath != nil {
		return fmt.Sprintf("  %s\n", t("check.candidate_renamed", path, displayOptionalPath(item.BasePath), item.BaseLines, item.TargetLines))
	}
	return fmt.Sprintf("  %s\n", t("check.candidate", path, item.BaseLines, item.TargetLines))
}
