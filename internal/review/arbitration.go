package review

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
)

// runArbitrationCommand records already-produced third-party arbitration
// evidence. The arbiter invocation itself intentionally reuses the caller's
// configured/native agent harness; this command is the durable evidence gate.
func runArbitrationCommand(args []string) int {
	if len(args) != 3 {
		return dispositionFailure(archiveError("usage: kander review arbitrate <CWD> <absolute-arbitration.json>"))
	}
	cwd, input := args[1], args[2]
	if !filepath.IsAbs(cwd) {
		return dispositionFailure(archiveError("absolute CWD required"))
	}
	root, err := board.BoardRootAt(cwd)
	if err != nil {
		return dispositionFailure(err)
	}
	var a board.ReviewArbitration
	if err = readArchiveJSON(input, &a); err == nil {
		err = board.SubmitReviewArbitration(root, a)
	}
	if err != nil {
		return dispositionFailure(err)
	}
	result, err := json.MarshalIndent(map[string]string{"arbitration_id": a.ArbitrationID}, "", "  ")
	if err != nil {
		return dispositionFailure(err)
	}
	fmt.Println(string(result))
	return 0
}
