package board

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

func renameEntry(root string, entry Entry, targetState string) (string, error) {
	target := filepath.Join(root, targetState, filepath.Base(entry.Path))
	err := fs.Rename(root, entry.Path, target)
	if err != nil {
		if isExist(err) {
			return "", kanbanError("board.target_already_exists", target)
		}
		if errors.Is(err, fs.ErrUnsafe) {
			return "", kanbanError(
				"board.task_path_must_not_contain_a_symlink_reparse_point_2", err.Error(),
			)
		}
		return "", kanbanError("board.move_failed", err.Error())
	}
	if existsNoFollow(entry.Path) || !existsNoFollow(target) || fs.IsReparsePoint(target) {
		return "", kanbanError("board.post_move_state_verification_failed_preserve_the_current_state")
	}
	return target, nil
}

// MoveEntry 校验转移并迁移入口; 进 done 时写入完成时间, 失败则回滚.
func MoveEntry(entry Entry, root, targetState string) (Entry, error) {
	if !allowedMove(entry.State, targetState) {
		return Entry{}, kanbanError("board.move_not_allowed", entry.State, targetState)
	}
	text, err := ReadDocument(entry)
	if err != nil {
		return Entry{}, err
	}
	if err := validateTarget(entry, targetState, text); err != nil {
		return Entry{}, err
	}
	updated := text
	if targetState == "done" {
		updated, err = completionMetadata(text)
		if err != nil {
			return Entry{}, err
		}
	}
	target, err := renameEntry(root, entry, targetState)
	if err != nil {
		return Entry{}, err
	}
	document := target
	if entry.Kind == "large" {
		document = filepath.Join(target, "spec.md")
	}
	moved := Entry{TaskID: entry.TaskID, State: targetState, Path: target, Document: document, Kind: entry.Kind}
	if updated != text {
		if err := writeDocument(moved, updated); err != nil {
			if _, rollbackErr := renameEntry(root, moved, entry.State); rollbackErr != nil {
				return Entry{}, kanbanError(
					"board.failed_to_record_completion_time_and_rollback_task_remains", err.Error(), rollbackErr.Error(),
				)
			}
			return Entry{}, kanbanError("board.failed_to_record_completion_time", err.Error())
		}
	}
	return moved, nil
}

// NewTask 在 backlog 创建小任务或大任务目录卡.
func NewTask(root, kind, slug, title string, large bool) (string, error) {
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
	board, err := LoadBoard(root)
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
	contract := renderContract(title, kind)
	if large {
		target := filepath.Join(root, "backlog", taskID)
		if err := fs.CreateDirectoryWithTextFile(root, target, "spec.md", contract); err != nil {
			if isExist(err) {
				return "", kanbanError("board.task_already_exists", taskID)
			}
			return "", kanbanError(
				"board.task_path_must_not_contain_a_symlink_reparse_point_2", err.Error(),
			)
		}
		return target, nil
	}
	target := filepath.Join(root, "backlog", taskID+".md")
	if err := fs.WriteTextAtomic(root, target, contract+smallTaskExtra(), false); err != nil {
		if isExist(err) {
			return "", kanbanError("board.task_already_exists", taskID)
		}
		return "", kanbanError(
			"board.task_path_must_not_contain_a_symlink_reparse_point_2", err.Error(),
		)
	}
	return target, nil
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
