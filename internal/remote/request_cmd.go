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
	"github.com/dualface/kander/internal/config"
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
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	switch args[0] {
	case "show":
		if len(args) != 4 || args[3] != "--json" {
			return requestUsage()
		}
		requests, err := s.Project.Requests(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		for _, request := range requests {
			if request.ID == args[2] {
				return encode(request)
			}
		}
		return fail(&api.Error{Code: api.CodeNotFound, Message: "request does not exist"})
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
		requests, err := s.Project.Requests(ctx, args[1])
		if err != nil {
			return fail(err)
		}
		var revision uint64
		for _, request := range requests {
			if request.ID == args[2] {
				revision = request.Revision
			}
		}
		if revision == 0 {
			return fail(&api.Error{Code: api.CodeNotFound, Message: "request does not exist"})
		}
		r, err := s.Project.AnswerRequest(ctx, client.NewKey(), args[1], args[2], api.CollabAnswer{ExpectedRevision: revision, Status: args[3], Note: strings.Join(args[4:], " ")}, *credential.Claim())
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, r.ID, r.Status)
		return 0
	}
	if len(args) < 3 || !slices.Contains([]string{api.RequestFix, api.RequestSync, api.RequestReview, api.RequestStop}, args[1]) {
		return requestUsage()
	}
	logical := struct{ Task, Kind, Message string }{args[0], args[1], strings.Join(args[2:], " ")}
	if recovered, found, err := s.RecoverRequest(ctx, logical); found || err != nil {
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, recovered.ID, "g"+strconv.FormatUint(recovered.TargetGeneration, 10), recovered.Status)
		return 0
	}
	task, err := s.Project.Show(ctx, args[0])
	if err != nil {
		return fail(err)
	}
	if task.Claim == nil {
		return fail(errors.New("task " + task.TaskID + " has no holder"))
	}
	r, err := s.SendRequest(ctx, client.NewKey(), task.TaskID, api.NewCollabRequest{Kind: args[1], Message: strings.Join(args[2:], " "), TargetGeneration: task.Claim.Generation}, logical)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, r.ID, "g"+strconv.FormatUint(r.TargetGeneration, 10), r.Status)
	return 0
}

func requestUsage() int {
	fmt.Fprintln(stderr, config.CommandUsage("request"))
	return 2
}
