package review

import (
	"fmt"
	"os"

	"github.com/dualface/kander/v2/internal/board"
)

// moveCard moves one card of the current test board and reports a failure on
// stderr; it returns 0 on success and 1 otherwise.
func moveCard(id, state string, options board.MoveOptions) int {
	root, err := board.BoardRoot()
	if err == nil {
		var loaded board.Board
		if loaded, err = board.LoadBoard(root); err == nil {
			var entry board.Entry
			if entry, err = board.Locate(loaded, id); err == nil {
				_, err = board.MoveWithOptions(entry, root, state, options)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	return 0
}
