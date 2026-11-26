package board

import (
	"path/filepath"
	"sort"
	"strings"
)

// NewTaskInput supplies the initial card contract and its submission identity.
// Language must already satisfy config.ValidateAgentLanguage.
type NewTaskInput struct {
	Kind, Slug, Title, Language, Submitter string
	Description                            string
	Large                                  bool
}

func newTask(tx *Transaction, root string, input NewTaskInput) (string, error) {
	kind, slug, title := input.Kind, input.Slug, strings.TrimSpace(input.Title)
	if !slugRe.MatchString(slug) {
		return "", kanbanError("board.slug_may_contain_only_lowercase_ascii_letters_digits_and")
	}
	if title == "" || strings.ContainsAny(title, "\n\r") {
		return "", kanbanError("board.title_must_not_be_empty_or_contain_newlines")
	}
	if _, ok := typeNames[kind]; !ok {
		return "", kanbanError("board.unknown_task_type", kind)
	}
	submitter, err := personField(FieldSubmitter, input.Submitter)
	if err != nil {
		return "", err
	}
	description, err := normalizeRequestDescription(input.Description)
	if err != nil {
		return "", err
	}
	board, err := scan(root)
	if err != nil {
		return "", err
	}
	taskID := todayPrefix() + "-" + slug + "-task"
	if _, ok := board.Entries[taskID]; ok {
		return "", kanbanError("board.task_already_exists", taskID)
	}
	if _, ok := board.Blocked[taskID]; ok {
		return "", kanbanError("board.task_already_exists", taskID)
	}
	contract := renderContract(title, kind, input.Language, submitter, description != "")

	taskKind := "small"
	target := filepath.Join(root, "backlog", taskID)
	if input.Large {
		taskKind = "large"
	} else {
		contract += smallTaskExtra()
	}
	contract, err = setMetadata(contract, FieldSize, taskKind)
	if err != nil {
		return "", err
	}
	if err := tx.touch(taskID); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if description != "" {
		// Recovery writes files before creating spec.md, so the marker never
		// becomes visible ahead of its record.
		tx.record.Directories = append(tx.record.Directories, rel)
		tx.record.Files = append(tx.record.Files, FileChange{Path: filepath.Join(rel, requestDocument), After: description})
	}
	tx.record.Entries = append(tx.record.Entries, EntryChange{To: rel, Kind: "large", Text: contract})
	tx.SetOperationResult(Snapshot{
		Entry: Entry{TaskID: taskID, State: "backlog", Path: target, Document: filepath.Join(target, "spec.md"), Kind: taskKind},
		Text:  contract, Revision: tx.record.Revisions[taskID], OperationID: tx.record.ID,
	})
	return target, nil
}

// NewTask creates an entry under the board and task locks, including identity allocation.
func NewTask(root, kind, slug, title, language string, large bool) (target string, err error) {
	return NewTaskWithTransaction(root, NewTaskInput{Kind: kind, Slug: slug, Title: title, Language: language, Large: large})
}

// NewTaskWithTransaction creates a card and lets a service stage its exact
// initial Snapshot from OperationResult in the same redo transaction.
func NewTaskWithTransaction(root string, input NewTaskInput, extensions ...TransactionExtension) (target string, err error) {
	id := todayPrefix() + "-" + input.Slug + "-task"
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		var e error
		target, e = newTask(tx, root, input)
		return e
	}, extensions...)
	return target, err
}

func selectedEntries(entries map[string]Entry, state string) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if state == "" || entry.State == state {
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := stateIndex(out[i].State), stateIndex(out[j].State)
		if si != sj {
			return si < sj
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out
}
