package remote

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// RunRequest implements kander request, the collaboration requests of a
// claimed task in server mode:
//
//	kander request <task-id> fix|sync|review|stop <message...>
//	kander request list <task-id>
//	kander request answer <task-id> <request-id> accepted|done|rejected [note...]
//
// Anyone may ask; only the live holder answers, with its execution credential.
func RunRequest(args []string) int {
	if len(args) < 2 {
		return requestUsage()
	}
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return requestUsage()
		}
		requests, err := s.Project.Requests(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		w := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
		for _, r := range requests {
			fmt.Fprintf(w, "%s\t%s\tg%d\t%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.TargetGeneration, r.Status, r.FromClient, r.CreatedAt.Local().Format(time.DateTime), r.Message)
		}
		w.Flush()
		return 0
	case "answer":
		if len(args) < 4 {
			return requestUsage()
		}
		credential, found, err := s.Credential(args[1])
		if err != nil {
			return fail(err)
		}
		if !found {
			return fail(&api.Error{Code: api.CodeHeldByOther, Message: "this host holds no claim of " + args[1]})
		}
		r, err := s.Project.AnswerRequest(ctx, client.NewKey(), args[1], args[2], api.CollabAnswer{Status: args[3], Note: strings.Join(args[4:], " ")}, *credential.Claim())
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, r.ID, r.Status)
		return 0
	}
	if len(args) < 3 || !slices.Contains([]string{api.RequestFix, api.RequestSync, api.RequestReview, api.RequestStop}, args[1]) {
		return requestUsage()
	}
	task, err := s.Project.Show(ctx, args[0])
	if err != nil {
		return fail(err)
	}
	if task.Claim == nil {
		return fail(errors.New("task " + task.TaskID + " has no holder"))
	}
	r, err := s.Project.CreateRequest(ctx, client.NewKey(), task.TaskID, api.NewCollabRequest{Kind: args[1], Message: strings.Join(args[2:], " "), TargetGeneration: task.Claim.Generation})
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, r.ID, "g"+strconv.FormatUint(r.TargetGeneration, 10), r.Status)
	return 0
}

func requestUsage() int {
	fmt.Fprintln(stderr, "usage: kander request <task-id> fix|sync|review|stop <message...>\n       kander request list <task-id>\n       kander request answer <task-id> <request-id> accepted|done|rejected [note...]")
	return 2
}
