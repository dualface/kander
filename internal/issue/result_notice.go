package issue

import (
	"fmt"
	"io"
	"strconv"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func init() { board.RegisterMoveObserver(observeResultHandoff) }

// observeResultHandoff watches a CLI move into a terminal state. When that move
// turns an issue's work set from not ready into ready, the notice names the
// result protocol entry so the moving session can run the completion handoff.
// Any read failure only skips the notice; the move itself never depends on it.
func observeResultHandoff(root string, entry board.Entry, target string) func(io.Writer, board.Entry) {
	if target != "done" && target != "archived" && target != "trash" {
		return nil
	}
	view, err := board.Scan(root)
	if err != nil {
		return nil
	}
	index := indexFrom(view)
	key := ""
	for candidate, set := range workSetsFrom(view, index) {
		if set.Contains(entry.TaskID) {
			if set.Ready() {
				return nil
			}
			key = candidate
			break
		}
	}
	if key == "" {
		return nil
	}
	return func(w io.Writer, _ board.Entry) {
		writeResultHandoffNotice(w, root, key)
	}
}

func writeResultHandoffNotice(w io.Writer, root, key string) {
	view, err := board.Scan(root)
	if err != nil {
		return
	}
	index := indexFrom(view)
	set, ok := workSetsFrom(view, index)[key]
	if !ok || !set.Ready() {
		return
	}
	cfg, err := config.Load(false)
	if err != nil {
		fmt.Fprintln(w, config.Text("issue.result_handoff_unavailable", err.Error()))
		return
	}
	mode := cfg.Issue.ResultOnDone
	if mode == config.IssueResultOff {
		return
	}
	local := index[key]
	repository := local.Repository.Host + "/" + local.Repository.Owner + "/" + local.Repository.Name
	done, _ := set.Progress()
	fmt.Fprintln(w, config.Text("issue.result_handoff_ready", key, set.Anchor.TaskID, done, len(set.Excluded), mode))
	fmt.Fprintln(w, config.Text("issue.result_handoff_next", "kander issue result "+strconv.Itoa(local.Number)+" --repo "+repository+" --card "+set.Anchor.TaskID+" --action inspect"))
	fmt.Fprintln(w, "Run \"Completion Handoff\" in KANDER-ISSUE-RULES.md with issue.result_on_done="+mode+".")
}
