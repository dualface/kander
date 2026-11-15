package board

import (
	"encoding/json"
	"errors"
	"os"
	"unicode/utf8"
)

// documentView is the `kander show --json --document` payload. Its field names
// match the document object of the v2 server API.
type documentView struct {
	Path     string `json:"path"`
	Text     string `json:"text"`
	Revision uint64 `json:"revision"`
	Entry    Entry  `json:"entry"`
}

var errOptionGivenTwice = errors.New("option given twice")

// afterDocumentSnapshot lets a test act between the snapshot and the document
// read of ReadDocumentSnapshot; it is nil in production.
var afterDocumentSnapshot func()

// takeOnceValueFlag is takeValueFlag for an option that may be given at most once.
func takeOnceValueFlag(args []string, name string) (rest []string, value string, found bool, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != name {
			rest = append(rest, args[i])
			continue
		}
		if found {
			return nil, "", true, errOptionGivenTwice
		}
		if i+1 >= len(args) {
			return nil, "", true, errors.New(name)
		}
		value, found = args[i+1], true
		i++
	}
	return rest, value, found, nil
}

// ReadDocumentSnapshot reads one document of a card together with the snapshot
// it belongs to. The snapshot and the document come from one read-only
// transaction, so the revision always describes the returned text. The path
// rules are those of the document update entrance; the read never follows a
// symlink.
func ReadDocumentSnapshot(root, id, name string) (Snapshot, string, error) {
	id, err := NormalizeTaskID(id)
	if err != nil {
		return Snapshot{}, "", err
	}
	var (
		s    Snapshot
		text string
	)
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		if s, e = tx.Snapshot(id); e != nil {
			return e
		}
		if afterDocumentSnapshot != nil {
			afterDocumentSnapshot()
		}
		if text, e = tx.Read(id, name); e != nil {
			if errors.Is(e, os.ErrNotExist) {
				return kanbanError("board.document_not_found", name)
			}
			return e
		}
		if !utf8.ValidString(text) {
			return kanbanError("board.task_document_is_not_valid_utf_8", name)
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, "", err
	}
	return s, text, nil
}

// printDocument writes the document: its bytes as they are, or the JSON view.
func printDocument(s Snapshot, name, text string, machine bool) error {
	if !machine {
		_, err := os.Stdout.WriteString(text)
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(documentView{Path: name, Text: text, Revision: s.Revision, Entry: s.Entry})
}
