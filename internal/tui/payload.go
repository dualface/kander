package tui

import (
	"github.com/dualface/kander/v2/internal/board"
)

func loadBoardPayload(root string) (BoardPayload, error) {
	return board.BoardPayload(root)
}

func loadTaskPayload(root, taskID string) (Task, error) {
	return board.TaskPayload(root, taskID)
}
