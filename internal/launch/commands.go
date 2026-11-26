package launch

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/dualface/kander/v2/internal/board"
)

func selectTodo(b board.Board, task string) (board.Entry, error) {
	if task != "" {
		entry, err := locateFn(b, task)
		if err != nil {
			return board.Entry{}, err
		}
		if entry.State != "todo" {
			return board.Entry{}, launchError(
				"launch.start_only_accepts_tasks_in_todo_is_in", entry.TaskID, entry.State,
			)
		}
		return entry, nil
	}
	var candidates []board.Entry
	for _, entry := range b.Entries {
		if entry.State == "todo" {
			candidates = append(candidates, entry)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TaskID < candidates[j].TaskID })
	if len(candidates) == 0 {
		return board.Entry{}, launchError("launch.no_tasks_in_todo")
	}
	for i, entry := range candidates {
		text, err := readDocumentFn(entry)
		if err != nil {
			return board.Entry{}, err
		}
		fmt.Printf("%d. %s\t%s\n", i+1, entry.TaskID, board.TitleFrom(text))
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(t("board.choose_a_task_number"))
		line, err := reader.ReadString('\n')
		if err != nil {
			return board.Entry{}, launchError("board.no_task_selected_specify_task_id")
		}
		choice := strings.TrimSpace(line)
		n, convErr := strconv.Atoi(choice)
		if convErr == nil && n >= 1 && n <= len(candidates) {
			return candidates[n-1], nil
		}
		fmt.Print(t("board.enter_a_number_from_1_to", strconv.Itoa(len(candidates))))
	}
}

func parentDir(p string) string {
	dir := p
	if i := lastSlash(dir); i >= 0 {
		return dir[:i]
	}
	return dir
}

func lastSlash(p string) int {
	i := strings.LastIndex(p, "/")
	j := strings.LastIndex(p, `\`)
	if j > i {
		return j
	}
	return i
}
