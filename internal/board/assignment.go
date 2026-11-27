package board

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxPersonFieldBytes = 256

// personField treats these values as opaque text, not as agent names or IDs.
// Reject line breaks and controls before trimming whitespace.
func personField(field, value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) {
		return "", kanbanError("board.person_field_invalid", field, maxPersonFieldBytes)
	}
	value = strings.TrimSpace(value)
	if len(value) > maxPersonFieldBytes {
		return "", kanbanError("board.person_field_invalid", field, maxPersonFieldBytes)
	}
	return value, nil
}

// NormalizePersonField validates and trims a person name the way the person
// fields of a card are: SUBMITTER, ASSIGNEE, and the names of decisions.
func NormalizePersonField(field, value string) (string, error) {
	return personField(field, value)
}

// NormalizeAssignee validates and trims the value stored in ASSIGNEE.
func NormalizeAssignee(value string) (string, error) {
	return personField(FieldAssignee, value)
}

// Assign changes only ASSIGNEE. It is a board management write independent of
// the executing agent, so neither a dispatch nor a contract decision is needed.
func Assign(root, id, assignee string, expected *uint64, extension TransactionExtension) error {
	id, err := NormalizeTaskID(id)
	if err != nil {
		return err
	}
	assignee, err = NormalizeAssignee(assignee)
	if err != nil {
		return err
	}
	return WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		var s Snapshot
		var err error
		if expected == nil {
			s, err = tx.Snapshot(id)
		} else {
			s, err = tx.Expect(id, "", *expected)
		}
		if err != nil {
			return err
		}
		if s.Entry.State == "trash" {
			return kanbanError("board.assign_trash", id)
		}
		text, err := setMetadata(s.Text, FieldAssignee, assignee)
		if err != nil {
			return err
		}
		return tx.Put(id, "spec.md", text)
	}, extension)
}
