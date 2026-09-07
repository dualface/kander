package board

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// newTask creates a directory card with an independent task size in backlog. language is written to the
// card's LANGUAGE field and must already satisfy config.ValidateAgentLanguage.
func newTask(tx *Transaction, root, kind, slug, title, language string, large bool) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", kanbanError("board.slug_may_contain_only_lowercase_ascii_letters_digits_and")
	}
	title = strings.TrimSpace(title)
	if title == "" || strings.ContainsAny(title, "\n\r") {
		return "", kanbanError("board.title_must_not_be_empty_or_contain_newlines")
	}
	if _, ok := typeNames[kind]; !ok {
		return "", kanbanError("board.unknown_task_type", kind)
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
	contract := renderContract(title, kind, language)

	taskKind := "small"
	target := filepath.Join(root, "backlog", taskID)
	if large {
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
	rel, _ := filepath.Rel(root, target)
	tx.record.Entries = append(tx.record.Entries, EntryChange{To: rel, Kind: "large", Text: contract})
	return target, nil
}

// NewTask creates an entry under the board and task locks, including identity allocation.
func NewTask(root, kind, slug, title, language string, large bool) (target string, err error) {
	id := todayPrefix() + "-" + slug + "-task"
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		var e error
		target, e = newTask(tx, root, kind, slug, title, language, large)
		return e
	})
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

func selectFromState(board Board, state string, stdin *os.File, stdout *os.File) (Entry, error) {
	candidates := selectedEntries(board.Entries, state)
	if len(candidates) == 0 {
		return Entry{}, kanbanError("board.no_tasks_in", state)
	}
	for i, entry := range candidates {
		text, err := ReadDocument(entry)
		if err != nil {
			return Entry{}, err
		}
		_, _ = stdout.WriteString(itoa(i+1) + ". " + entry.TaskID + "\t" + TitleFrom(text) + "\n")
	}
	reader := bufio.NewReader(stdin)
	for {
		_, _ = stdout.WriteString(t("board.choose_a_task_number"))
		line, err := reader.ReadString('\n')
		if err != nil {
			return Entry{}, kanbanError("board.no_task_selected_specify_task_id")
		}
		choice := strings.TrimSpace(line)
		n := 0
		ok := true
		for _, r := range choice {
			if r < '0' || r > '9' {
				ok = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if ok && choice != "" && n >= 1 && n <= len(candidates) {
			return candidates[n-1], nil
		}
		_, _ = stdout.WriteString(t("board.enter_a_number_from_1_to", itoa(len(candidates))))
	}
}
