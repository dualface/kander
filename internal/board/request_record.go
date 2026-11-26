package board

import (
	"strings"
	"unicode/utf8"
)

const (
	requestDocument            = "request.md"
	maxRequestDescriptionBytes = 65536
)

// IsRequestCard reports whether the card header names a request record.
func IsRequestCard(text string) bool {
	return MetadataFrom(text, FieldRequest) != ""
}

func normalizeRequestDescription(description string) (string, error) {
	if description == "" {
		return "", nil
	}
	invalid := func(reason string, args ...any) (string, error) {
		return "", kanbanError("board.request_description_invalid", t(reason, args...))
	}
	if !utf8.ValidString(description) {
		return invalid("board.request_description_utf8")
	}
	description = lfLineEndings(description)
	if len(description) > maxRequestDescriptionBytes {
		return invalid("board.request_description_size", maxRequestDescriptionBytes)
	}
	for _, char := range description {
		if char < 0x20 && char != '\t' && char != '\n' || char >= 0x7f && char <= 0x9f {
			return invalid("board.request_description_control")
		}
	}
	if strings.TrimSpace(description) == "" {
		return invalid("board.request_description_blank")
	}
	return description, nil
}
