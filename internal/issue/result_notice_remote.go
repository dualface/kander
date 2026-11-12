package issue

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func init() { remote.RegisterMoveObserver(observeRemoteResultHandoff) }

func observeRemoteResultHandoff(ctx context.Context, session *remote.Session, task api.Task, target string) func(io.Writer, api.Task) {
	if target != "done" && target != "archived" && target != "trash" {
		return nil
	}
	before, err := RemoteIndex(ctx, session)
	if err != nil {
		return nil
	}
	return func(w io.Writer, _ api.Task) {
		after, err := RemoteIndex(ctx, session)
		if err != nil {
			return
		}
		cfg, err := config.Load(false)
		if err != nil {
			fmt.Fprintln(w, config.Text("issue.result_handoff_unavailable", err.Error()))
			return
		}
		if cfg.Issue.ResultOnDone == config.IssueResultOff {
			return
		}
		for _, key := range slices.Sorted(maps.Keys(after)) {
			card := after[key]
			previous, found := before[key]
			if !found || previous.WorkReady || !card.WorkReady {
				continue
			}
			// The central ready transition identifies the issue; only the moving
			// anchor or an explicitly linked sibling may cause this CLI notice.
			related := card.TaskID == task.TaskID
			if !related {
				for _, ref := range sourceIssueRefs(task.Text) {
					if ref[0] == key && ref[1] == card.TaskID {
						related = true
						break
					}
				}
			}
			if !related {
				continue
			}
			repository := card.Repository.Host + "/" + card.Repository.Owner + "/" + card.Repository.Name
			fmt.Fprintln(w, config.Text("issue.result_handoff_ready", key, card.TaskID, card.WorkDone, card.WorkExcluded, cfg.Issue.ResultOnDone))
			fmt.Fprintln(w, config.Text("issue.result_handoff_next", "kander issue result "+strconv.Itoa(card.Number)+" --repo "+repository+" --card "+card.TaskID+" --action inspect"))
			fmt.Fprintln(w, "Run \"Completion Handoff\" in KANDER-ISSUE-RULES.md with issue.result_on_done="+cfg.Issue.ResultOnDone+".")
		}
	}
}
