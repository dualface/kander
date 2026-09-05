package tui

import (
	"github.com/dualface/kander/internal/board"
)

func loadBoardPayload(root string) (BoardPayload, error) {
	return board.BoardPayload(root)
}

func loadTaskPayload(root, taskID string) (Task, error) {
	return board.TaskPayload(root, taskID)
}
