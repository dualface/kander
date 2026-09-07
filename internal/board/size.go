package board

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	markdowntext "github.com/yuin/goldmark/text"
	"strings"
)

// IsDirectory reports storage form; Kind is populated with task size when the document is attached.
func (e Entry) IsDirectory() bool { return !strings.HasSuffix(e.Path, ".md") }

// storageKind preserves schema-1 transaction entry form encoding.
func (e Entry) storageKind() string {
	if e.IsDirectory() {
		return "large"
	}
	return "small"
}

func taskSize(e Entry, text string) (string, error) {
	lines := fieldLines(text, FieldSize)
	if len(lines) == 0 {
		return e.storageKind(), nil
	}
	size := MetadataFrom(text, FieldSize)
	if len(lines) != 1 || (size != "small" && size != "large") {
		return size, kanbanError("board.size_invalid", e.TaskID)
	}
	return size, nil
}

func attachSize(e Entry, text string) Entry {
	size, err := taskSize(e, text)
	if err != nil {
		e.Kind = "invalid"
	} else {
		e.Kind = size
	}
	return e
}

// ValidateMutable rejects legacy file mutations and ambiguous sizes before any
// launch, delivery or write side effect. Readers retain legacy compatibility.
func ValidateMutable(e Entry, text string) error {
	if _, err := taskSize(e, text); err != nil {
		return err
	}
	if !e.IsDirectory() {
		return kanbanError("board.migration_required", e.TaskID)
	}
	return nil
}

// addSize preserves every existing byte, inserting only one canonical line.
func addSize(text, size string) string {
	if len(fieldLines(text, FieldSize)) > 0 {
		return text
	}
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	line := "- SIZE: " + size + newline
	if at := FieldLineRe(FieldType).FindStringIndex(text); at != nil {
		end := at[1]
		if end < len(text) && text[end] == '\n' {
			end++
		} else {
			line = newline + line
		}
		return text[:end] + line + text[end:]
	}
	// Only a top-level parsed ATX H1 can anchor missing-TYPE metadata.
	// Text resembling a heading inside code, lists or quotes is not an anchor.
	source := []byte(text)
	document := goldmark.DefaultParser().Parse(markdowntext.NewReader(source))
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level != 1 || heading.Lines().Len() == 0 {
			continue
		}
		start := heading.Lines().At(0).Start
		start = strings.LastIndexByte(text[:start], '\n') + 1
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		title := text[start:end]
		if !strings.HasPrefix(strings.TrimLeft(title, " "), "# ") {
			continue
		}
		if !strings.HasSuffix(title, "\n") {
			line = newline + line
		}
		return text[:end] + line + text[end:]
	}
	return line + text
}
