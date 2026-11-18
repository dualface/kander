package remote

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
)

// NewRequestCommand builds kander request, the collaboration requests of a claimed task in
// server mode:
//
//	kander request <task-id> fix|sync|review|stop <message...>
//	kander request list <task-id>
//	kander request show <task-id> <request-id>
//	kander request answer <task-id> <request-id> accepted|done|rejected [note...]
//
// Anyone may ask; only the live holder answers, with its execution credential.
func NewRequestCommand() *cobra.Command {
	send := command.RequireProject(&cobra.Command{
		Use: "request <task-id> fix|sync|review|stop <message...>",
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(3, -1)(c, args); err != nil {
				return err
			}
			if !slices.Contains([]string{api.RequestFix, api.RequestSync, api.RequestReview, api.RequestStop}, args[1]) {
				return command.Usage("remote.request_kind_invalid", command.Escape(args[1]))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runRequestSend(args[0], args[1], strings.Join(args[2:], " "))
		},
	})
	list := &cobra.Command{
		Use:  "list <task-id>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error { return runRequestList(args[0]) },
	}
	show := &cobra.Command{
		Use:  "show <task-id> <request-id>",
		Args: command.Args(2, 2),
		RunE: func(_ *cobra.Command, args []string) error { return runRequestShow(args[0], args[1]) },
	}
	answer := &cobra.Command{
		Use: "answer <task-id> <request-id> accepted|done|rejected [note...]",
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(3, -1)(c, args); err != nil {
				return err
			}
			if !slices.Contains([]string{api.RequestAccepted, api.RequestDone, api.RequestRejected}, args[2]) {
				return command.Usage("cli.error_invalid_value", "<status>", command.Escape(args[2]))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runRequestAnswer(args[0], args[1], args[2], strings.Join(args[3:], " "))
		},
	}
	send.AddCommand(command.RequireProject(list), command.RequireProject(show), command.RequireProject(answer))
	return send
}

// openRequestSession opens the project session of one request operation; release must be
// deferred by the caller.
func openRequestSession() (ctx context.Context, s *Session, release func(), err error) {
	base, cancel := commandContext()
	s, _, err = open(base)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	ctx, finish := s.OperationContext(base)
	return ctx, s, func() { finish(); cancel() }, nil
}

func runRequestShow(task, id string) error {
	ctx, s, release, err := openRequestSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	requests, err := s.Project.Requests(ctx, task)
	if err != nil {
		return fail(err)
	}
	for _, request := range requests {
		if request.ID == id {
			return emit(request)
		}
	}
	return fail(&api.Error{Code: api.CodeNotFound, Message: "request does not exist"})
}

func runRequestList(task string) error {
	ctx, s, release, err := openRequestSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	requests, err := s.Project.Requests(ctx, task)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(requestList{Requests: requests}))
	}
	w := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	for _, r := range requests {
		fmt.Fprintf(w, "%s\t%s\tg%d\t%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.TargetGeneration, r.Status, r.FromClient, r.CreatedAt.Local().Format(time.DateTime), r.Message)
	}
	w.Flush()
	return nil
}

func runRequestAnswer(task, id, status, note string) error {
	ctx, s, release, err := openRequestSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	credential, found, err := s.Credential(task)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(&api.Error{Code: api.CodeHeldByOther, Message: "this host holds no claim of " + task})
	}
	requests, err := s.Project.Requests(ctx, task)
	if err != nil {
		return fail(err)
	}
	var revision uint64
	for _, request := range requests {
		if request.ID == id {
			revision = request.Revision
		}
	}
	if revision == 0 {
		return fail(&api.Error{Code: api.CodeNotFound, Message: "request does not exist"})
	}
	r, err := s.Project.AnswerRequest(ctx, client.NewKey(), task, id, api.CollabAnswer{ExpectedRevision: revision, Status: status, Note: note}, *credential.Claim())
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(r))
	}
	fmt.Fprintln(stdout, r.ID, r.Status)
	return nil
}

func runRequestSend(task, kind, message string) error {
	ctx, s, release, err := openRequestSession()
	if err != nil {
		return fail(err)
	}
	defer release()
	logical := struct{ Task, Kind, Message string }{task, kind, message}
	if recovered, found, err := s.RecoverRequest(ctx, logical); found || err != nil {
		if err != nil {
			return fail(err)
		}
		return printRequest(recovered)
	}
	current, err := s.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	if current.Claim == nil {
		return fail(errors.New("task " + current.TaskID + " has no holder"))
	}
	r, err := s.SendRequest(ctx, client.NewKey(), current.TaskID, api.NewCollabRequest{Kind: kind, Message: message, TargetGeneration: current.Claim.Generation}, logical)
	if err != nil {
		return fail(err)
	}
	return printRequest(r)
}

// printRequest ends a command that queued a request: its id, target generation and status, or
// with --json the request.
func printRequest(r api.CollabRequest) error {
	if command.JSON() {
		return command.Exit(command.Result(r))
	}
	fmt.Fprintln(stdout, r.ID, "g"+strconv.FormatUint(r.TargetGeneration, 10), r.Status)
	return nil
}
