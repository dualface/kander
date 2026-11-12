package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
)

// SelectTask preserves the interactive pick/start selection over a service
// snapshot. The eventual mutation still validates the selected task revision.
func SelectTask(ctx context.Context, session *Session, state string, input io.Reader, output io.Writer) (api.TaskSummary, error) {
	list, err := session.Project.List(ctx, state)
	if err != nil {
		return api.TaskSummary{}, err
	}
	if len(list.Tasks) == 0 {
		return api.TaskSummary{}, errors.New(config.Text("board.no_tasks_in", state))
	}
	for i, task := range list.Tasks {
		if _, err := fmt.Fprintf(output, "%d. %s\t%s\n", i+1, task.TaskID, task.Title); err != nil {
			return api.TaskSummary{}, err
		}
	}
	reader := bufio.NewReader(io.LimitReader(input, 65536))
	for {
		if _, err := fmt.Fprint(output, config.Text("board.choose_a_task_number")); err != nil {
			return api.TaskSummary{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return api.TaskSummary{}, errors.New(config.Text("board.no_task_selected_specify_task_id"))
		}
		choice, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && choice >= 1 && choice <= len(list.Tasks) {
			return list.Tasks[choice-1], nil
		}
		if _, err := fmt.Fprintln(output, config.Text("board.enter_a_number_from_1_to", strconv.Itoa(len(list.Tasks)))); err != nil {
			return api.TaskSummary{}, err
		}
	}
}
