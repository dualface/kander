package board

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// ImportFinding is one board check problem that an import accepts: every card
// it concerns is closed.
type ImportFinding struct {
	TaskIDs []string
	Message string
}

// ImportCheck classifies the problems of a staged v1 board. A problem whose
// cards are all in done, archived or trash is a warning, because the board
// keeps those cards as history. Any other problem blocks the import, including
// one whose cards cannot be determined.
type ImportCheck struct {
	Warnings []ImportFinding
	// Blocking holds the formatted lines CheckBoardForImport reports.
	Blocking []string
}

var closedImportStates = map[string]struct{}{"done": {}, "archived": {}, "trash": {}}

// CheckBoardForImportProblems runs the checks of CheckBoardForImport and
// splits the problems into warnings and blocking lines.
func CheckBoardForImportProblems(root string) (ImportCheck, error) {
	board, problems, err := collectBoardProblems(root, nil, true, true)
	var check ImportCheck
	if err != nil {
		return check, err
	}
	for _, problem := range problems {
		if ids, ok := closedProblemTasks(board, problem); ok {
			check.Warnings = append(check.Warnings, ImportFinding{TaskIDs: ids, Message: problem.Message})
			continue
		}
		check.Blocking = append(check.Blocking, t("board.invalid", problem.Message))
	}
	return check, nil
}

// closedProblemTasks returns the cards a problem concerns and whether every
// one of them is closed. A path that names no card of the board makes the
// problem undeterminable, which counts as not closed.
func closedProblemTasks(board Board, problem Problem) ([]string, bool) {
	seen := map[string]struct{}{}
	for _, path := range append([]string{problem.Path}, problem.RelatedPaths...) {
		id, ok := problemTask(board, path)
		if !ok {
			return nil, false
		}
		seen[id] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		if _, closed := closedImportStates[board.Entries[id].State]; !closed {
			return nil, false
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, true
}

func problemTask(board Board, path string) (string, bool) {
	if _, ok := board.Entries[path]; ok {
		return path, true
	}
	for id, entry := range board.Entries {
		if path == entry.Path || path == entry.Document || strings.HasPrefix(path, entry.Path+string(filepath.Separator)) {
			return id, true
		}
	}
	return "", false
}

// SizeDefault names a directory card that received the default SIZE line.
type SizeDefault struct {
	TaskID string
	// Document is the slash-separated path of the card's spec.md below root.
	Document string
}

// DefaultSize is the SIZE a directory card without one is read with.
const DefaultSize = "large"

// DefaultedSizeText is the card text DefaultMissingSize writes for a card
// whose current text is before: one inserted SIZE line, every other byte kept.
func DefaultedSizeText(before string) string { return addSize(before, DefaultSize) }

// DefaultMissingSize inserts the single line "- SIZE: large" into the spec.md
// of every directory card that has no SIZE line, and changes nothing else.
// Legacy file cards are left alone, as they stay read-only until migration.
// It runs on a staged copy that no other writer touches, and returns the cards
// it changed ordered by task ID.
func DefaultMissingSize(root string) ([]SizeDefault, error) {
	board, err := Scan(root)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(board.Entries))
	for id := range board.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var changed []SizeDefault
	for _, id := range ids {
		entry := board.Entries[id]
		if !entry.IsDirectory() {
			continue
		}
		text, err := board.Document(id)
		if err != nil {
			return changed, err
		}
		if len(fieldLines(text, FieldSize)) > 0 {
			continue
		}
		rel, err := filepath.Rel(root, entry.Document)
		if err != nil || strings.HasPrefix(rel, "..") {
			return changed, errors.New("card document " + entry.Document + " is outside the board")
		}
		if err := fs.WriteTextAtomicInherited(root, entry.Document, DefaultedSizeText(text), true); err != nil {
			return changed, err
		}
		changed = append(changed, SizeDefault{TaskID: id, Document: filepath.ToSlash(rel)})
	}
	return changed, nil
}
