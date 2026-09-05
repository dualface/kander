package board

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

func reject(board *Board, path, message, taskID string, related ...string) {
	board.Problems = append(board.Problems, Problem{Path: path, Message: message, RelatedPaths: related})
	if taskID != "" {
		board.Blocked[taskID] = message
		delete(board.Entries, taskID)
	}
}

// Scan 枚举全部状态栏入口.
func Scan(root string) (Board, error) {
	if err := ensureLayout(root); err != nil {
		return Board{}, err
	}
	board := Board{
		Entries: map[string]Entry{},
		Blocked: map[string]string{},
	}
	for _, state := range States {
		statePath := filepath.Join(root, state)
		items, err := os.ReadDir(statePath)
		if err != nil {
			return Board{}, kanbanError("board.state_directory_does_not_exist", statePath)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })
		for _, item := range items {
			if strings.HasPrefix(item.Name(), ".") {
				continue
			}
			path := filepath.Join(statePath, item.Name())
			if fs.IsReparsePoint(path) {
				reject(&board, path, t("board.task_entry_must_not_be_a_symlink_reparse_point", path), "")
				continue
			}
			info, err := item.Info()
			if err != nil {
				reject(&board, path, t("board.invalid_task_entry", path), "")
				continue
			}
			var taskID, document, kind string
			switch {
			case info.Mode().IsRegular() && strings.HasSuffix(item.Name(), "-task.md"):
				taskID, document, kind = strings.TrimSuffix(item.Name(), ".md"), path, "small"
			case info.IsDir() && strings.HasSuffix(item.Name(), "-task"):
				taskID, document, kind = item.Name(), filepath.Join(path, "spec.md"), "large"
			default:
				reject(&board, path, t("board.invalid_task_entry", path), "")
				continue
			}
			if !taskIDRe.MatchString(taskID) {
				reject(&board, path, t("board.invalid_task_id", taskID), "")
				continue
			}
			if kind == "large" {
				if fs.IsReparsePoint(document) {
					reject(&board, path, t("board.large_task_spec_md_must_not_be_a_symlink", document), taskID)
					continue
				}
				if !isFileNoFollow(document) {
					reject(&board, path, t("board.large_task_is_missing_spec_md", path), taskID)
					continue
				}
			}
			if _, blocked := board.Blocked[taskID]; blocked {
				board.Problems = append(board.Problems, Problem{
					Path:    path,
					Message: t("board.task_id_conflict", taskID),
				})
				continue
			}
			if existing, ok := board.Entries[taskID]; ok {
				reject(&board, path, t(
					"board.duplicate_task_id_and", existing.Path, path,
				), taskID, existing.Path)
				continue
			}
			board.Entries[taskID] = Entry{TaskID: taskID, State: state, Path: path, Document: document, Kind: kind}
		}
	}
	return board, nil
}

func scanTargetsOnce(root string, taskIDs []string) (Board, bool) {
	board := Board{
		Entries: map[string]Entry{},
		Blocked: map[string]string{},
	}
	needsConfirmation := false
	for _, taskID := range taskIDs {
		var matches []Entry
		for _, state := range States {
			statePath := filepath.Join(root, state)
			smallPath := filepath.Join(statePath, taskID+".md")
			largePath := filepath.Join(statePath, taskID)

			if fs.IsReparsePoint(smallPath) {
				board.Problems = append(board.Problems, Problem{
					Path:    smallPath,
					Message: t("board.task_entry_must_not_be_a_symlink_reparse_point", smallPath),
				})
			} else {
				exists, err := fs.RegularFileExists(root, smallPath)
				if err != nil {
					board.Problems = append(board.Problems, Problem{
						Path:    smallPath,
						Message: t("board.task_entry_has_the_wrong_type", smallPath),
					})
				} else if exists {
					matches = append(matches, Entry{TaskID: taskID, State: state, Path: smallPath, Document: smallPath, Kind: "small"})
				}
			}

			if fs.IsReparsePoint(largePath) {
				board.Problems = append(board.Problems, Problem{
					Path:    largePath,
					Message: t("board.task_entry_must_not_be_a_symlink_reparse_point", largePath),
				})
				continue
			}
			largeExists, err := fs.DirectoryExists(root, largePath)
			if err != nil {
				board.Problems = append(board.Problems, Problem{
					Path:    largePath,
					Message: t("board.task_entry_has_the_wrong_type", largePath),
				})
				continue
			}
			if !largeExists {
				continue
			}
			document := filepath.Join(largePath, "spec.md")
			if fs.IsReparsePoint(document) {
				board.Problems = append(board.Problems, Problem{
					Path:    largePath,
					Message: t("board.large_task_spec_md_must_not_be_a_symlink", document),
				})
				continue
			}
			specExists, err := fs.RegularFileExists(root, document)
			if err != nil {
				board.Problems = append(board.Problems, Problem{
					Path:    largePath,
					Message: t("board.large_task_spec_md_is_not_a_regular_file", document),
				})
				continue
			}
			if !specExists {
				board.Problems = append(board.Problems, Problem{
					Path:    largePath,
					Message: t("board.large_task_is_missing_spec_md", largePath),
				})
				continue
			}
			matches = append(matches, Entry{TaskID: taskID, State: state, Path: largePath, Document: document, Kind: "large"})
		}
		var taskProblems []Problem
		for _, problem := range board.Problems {
			name := filepath.Base(problem.Path)
			if name == taskID || name == taskID+".md" {
				taskProblems = append(taskProblems, problem)
			}
		}
		if len(matches) > 1 {
			paths := make([]string, len(matches))
			for i, m := range matches {
				paths[i] = m.Path
			}
			message := t("board.duplicate_task_id", strings.Join(paths, t("board.and_separator")))
			board.Problems = append(board.Problems, Problem{Path: matches[len(matches)-1].Path, Message: message})
			board.Blocked[taskID] = message
			needsConfirmation = true
		} else if len(taskProblems) > 0 {
			board.Blocked[taskID] = taskProblems[0].Message
		} else if len(matches) == 1 {
			board.Entries[taskID] = matches[0]
		} else {
			message := t("board.task_does_not_exist", taskID)
			board.Problems = append(board.Problems, Problem{Path: root, Message: message})
			board.Blocked[taskID] = message
			needsConfirmation = true
		}
	}
	return board, needsConfirmation
}

// ScanTargets 只探测明确任务 ID.
func ScanTargets(root string, values []string) (Board, error) {
	if err := ensureLayout(root); err != nil {
		return Board{}, err
	}
	taskIDs := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		id, err := NormalizeTaskID(value)
		if err != nil {
			return Board{}, err
		}
		if _, ok := seen[id]; ok {
			return Board{}, kanbanError("board.target_task_ids_must_not_be_repeated")
		}
		seen[id] = struct{}{}
		taskIDs = append(taskIDs, id)
	}
	var board Board
	for i := 0; i < 3; i++ {
		var needs bool
		board, needs = scanTargetsOnce(root, taskIDs)
		if !needs {
			return board, nil
		}
	}
	return board, nil
}

// LoadBoard 扫描并在存在无效入口时向 stderr 警告.
func LoadBoard(root string) (Board, error) {
	board, err := Scan(root)
	if err != nil {
		return Board{}, err
	}
	if len(board.Problems) > 0 {
		n := len(board.Problems)
		os.Stderr.WriteString(t(
			"board.kander_warning_ignored_invalid_entries_run_kander_check_for", strconv.Itoa(n),
		))
	}
	return board, nil
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// NormalizeTaskID 校验任务参数.
func NormalizeTaskID(value string) (string, error) {
	if strings.ContainsAny(value, `/\`) {
		return "", kanbanError("board.task_argument_must_be_a_task_id_or_entry")
	}
	taskID := value
	if strings.HasSuffix(value, ".md") {
		taskID = value[:len(value)-3]
	}
	if !taskIDRe.MatchString(taskID) {
		return "", kanbanError("board.invalid_task_id", value)
	}
	return taskID, nil
}

// Locate 按任务 ID 查找入口.
func Locate(board Board, value string) (Entry, error) {
	taskID, err := NormalizeTaskID(value)
	if err != nil {
		return Entry{}, err
	}
	if msg, ok := board.Blocked[taskID]; ok {
		return Entry{}, &Error{Message: msg}
	}
	entry, ok := board.Entries[taskID]
	if !ok {
		return Entry{}, kanbanError("board.task_does_not_exist", taskID)
	}
	return entry, nil
}

func boardRootFromEntry(entry Entry) string {
	return filepath.Dir(filepath.Dir(entry.Path))
}
