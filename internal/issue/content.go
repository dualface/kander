package issue

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Content bounds of one issue. Bodies and comments must stay within these
// limits; the provider reports ErrorLimitExceeded instead of truncating them.
const (
	MaxIssueBodyBytes  = 512 << 10
	MaxCommentBytes    = 64 << 10
	MaxIssueComments   = 50
	MaxIssueTitleRunes = 1024
	MaxIssueLabelRunes = 100
	MaxIssueLabels     = 100
	MaxIssueStateRunes = 32
	MaxIssueAuthorRune = 256

	// MaxIssueSnapshotBytes bounds the body plus all comments of one snapshot.
	// A snapshot that exceeds it is rejected instead of being truncated, so an
	// imported card always carries the complete source.
	MaxIssueSnapshotBytes = 1 << 20
)

// truncation marker appended when a small display field is trimmed.
const truncatedSuffix = "..."

// SanitizeRemoteText removes terminal escape sequences (CSI and OSC), C0/C1
// control characters (keeping line feeds and tabs), bidi overrides and invalid
// UTF-8 from untrusted remote text. Line structure is preserved so Markdown
// rendering stays readable.
func SanitizeRemoteText(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	var builder strings.Builder
	builder.Grow(len(normalized))
	for index := 0; index < len(normalized); {
		r, size := utf8.DecodeRuneInString(normalized[index:])
		if r == 0x1b {
			skipped := escapeSpan(normalized[index:])
			if skipped < 1 {
				skipped = 1
			}
			index += skipped
			continue
		}
		if r == utf8.RuneError && size == 1 {
			index++
			continue
		}
		index += size
		switch {
		case r == '\n' || r == '\t':
			builder.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		case r >= 0x80 && r <= 0x9f:
		case isBidiControl(r):
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// escapeSpan returns the byte length of the escape sequence starting at data.
func escapeSpan(data string) int {
	if len(data) < 2 {
		return len(data)
	}
	switch data[1] {
	case '[':
		for index := 2; index < len(data); index++ {
			if data[index] >= 0x40 && data[index] <= 0x7e {
				return index + 1
			}
		}
		return len(data)
	case ']':
		for index := 2; index < len(data); index++ {
			if data[index] == 0x07 {
				return index + 1
			}
			if data[index] == 0x1b && index+1 < len(data) && data[index+1] == '\\' {
				return index + 2
			}
		}
		return len(data)
	default:
		return 2
	}
}

func isBidiControl(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200e || r == 0x200f || r == 0x061c:
		return true
	}
	return false
}

// cleanLine sanitizes one single-line display field, collapses its whitespace
// and truncates it to limit runes with an explicit marker.
func cleanLine(text string, limit int) string {
	sanitized := SanitizeRemoteText(text)
	fields := strings.FieldsFunc(sanitized, func(r rune) bool {
		return r == '\n' || r == '\t' || unicode.IsSpace(r)
	})
	joined := strings.Join(fields, " ")
	runes := []rune(joined)
	if len(runes) <= limit {
		return joined
	}
	if limit <= len(truncatedSuffix) {
		return string(runes[:limit])
	}
	return string(runes[:limit-len(truncatedSuffix)]) + truncatedSuffix
}

// boundedBody sanitizes a multi-line body and rejects it when it exceeds the
// byte limit instead of silently truncating remote content.
func boundedBody(text string, limit int, field string) (string, error) {
	sanitized := SanitizeRemoteText(text)
	if len(sanitized) > limit {
		return "", &Error{Kind: ErrorLimitExceeded, Op: field, Detail: strconv.Itoa(len(sanitized))}
	}
	return sanitized, nil
}

// NormalizeSummary sanitizes and bounds one list item.
func NormalizeSummary(summary IssueSummary) (IssueSummary, error) {
	if summary.Number <= 0 {
		return IssueSummary{}, NewError(ErrorInvalidResponse, "list", "issue number")
	}
	if summary.UpdatedAt.IsZero() {
		return IssueSummary{}, NewError(ErrorInvalidResponse, "list", "updated_at")
	}
	summary.Title = cleanLine(summary.Title, MaxIssueTitleRunes)
	summary.State = cleanLine(summary.State, MaxIssueStateRunes)
	summary.URL = cleanLine(summary.URL, 2048)
	labels, err := normalizeLabels(summary.Labels)
	if err != nil {
		return IssueSummary{}, err
	}
	summary.Labels = labels
	return summary, nil
}

// NormalizeComment sanitizes and bounds one comment.
func NormalizeComment(comment IssueComment) (IssueComment, error) {
	if comment.CreatedAt.IsZero() {
		return IssueComment{}, NewError(ErrorInvalidResponse, "comments", "created_at")
	}
	body, err := boundedBody(comment.Body, MaxCommentBytes, "comment")
	if err != nil {
		return IssueComment{}, err
	}
	comment.Body = body
	comment.Author = cleanLine(comment.Author, MaxIssueAuthorRune)
	comment.URL = cleanLine(comment.URL, 2048)
	return comment, nil
}

// NormalizeSnapshot sanitizes and bounds a full issue snapshot. The comment
// count is bounded here as well so every caller sees the same rule.
func NormalizeSnapshot(snapshot IssueSnapshot) (IssueSnapshot, error) {
	if snapshot.Number <= 0 {
		return IssueSnapshot{}, NewError(ErrorInvalidResponse, "show", "issue number")
	}
	if snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() {
		return IssueSnapshot{}, NewError(ErrorInvalidResponse, "show", "timestamps")
	}
	body, err := boundedBody(snapshot.Body, MaxIssueBodyBytes, "body")
	if err != nil {
		return IssueSnapshot{}, err
	}
	snapshot.Body = body
	snapshot.Title = cleanLine(snapshot.Title, MaxIssueTitleRunes)
	snapshot.State = cleanLine(snapshot.State, MaxIssueStateRunes)
	snapshot.Author = cleanLine(snapshot.Author, MaxIssueAuthorRune)
	snapshot.URL = cleanLine(snapshot.URL, 2048)
	labels, err := normalizeLabels(snapshot.Labels)
	if err != nil {
		return IssueSnapshot{}, err
	}
	snapshot.Labels = labels
	assignees := make([]string, 0, len(snapshot.Assignees))
	for _, raw := range snapshot.Assignees {
		assignees = append(assignees, cleanLine(raw, MaxIssueAuthorRune))
	}
	snapshot.Assignees = assignees
	if len(snapshot.Comments) > MaxIssueComments {
		return IssueSnapshot{}, &Error{Kind: ErrorLimitExceeded, Op: "comments", Detail: strconv.Itoa(len(snapshot.Comments))}
	}
	comments := make([]IssueComment, 0, len(snapshot.Comments))
	for _, comment := range snapshot.Comments {
		normalized, err := NormalizeComment(comment)
		if err != nil {
			return IssueSnapshot{}, err
		}
		comments = append(comments, normalized)
	}
	snapshot.Comments = comments
	total := len(snapshot.Body)
	for _, comment := range snapshot.Comments {
		total += len(comment.Body)
	}
	if total > MaxIssueSnapshotBytes {
		return IssueSnapshot{}, &Error{Kind: ErrorLimitExceeded, Op: "snapshot", Detail: strconv.Itoa(total)}
	}
	return snapshot, nil
}

func normalizeLabels(labels []string) ([]string, error) {
	if len(labels) > MaxIssueLabels {
		return nil, &Error{Kind: ErrorLimitExceeded, Op: "labels", Detail: strconv.Itoa(len(labels))}
	}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		cleaned := cleanLine(label, MaxIssueLabelRunes)
		if cleaned == "" {
			continue
		}
		out = append(out, cleaned)
	}
	return out, nil
}
