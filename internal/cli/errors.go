package cli

import (
	"errors"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
)

func init() {
	command.RegisterClassifier(classifyBoard)
	board.Remark = command.Notify
}

// classifyBoard gives a board rejection its board code, for example board.document_not_found,
// and its localized text.
func classifyBoard(err error) (code, message string, ok bool) {
	var rejected *board.Error
	if errors.As(err, &rejected) && rejected.Code != "" {
		return rejected.Code, rejected.Message, true
	}
	return "", "", false
}
