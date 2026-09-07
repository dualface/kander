package board

import "strings"

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
	// A malformed legacy card may lack TYPE; keep its H1 first without
	// inventing metadata or rewriting any existing line.
	offset := 0
	for _, title := range strings.SplitAfter(text, "\n") {
		offset += len(title)
		if strings.HasPrefix(title, "# ") {
			if !strings.HasSuffix(title, "\n") {
				line = newline + line
			}
			return text[:offset] + line + text[offset:]
		}
	}
	return line + text
}
