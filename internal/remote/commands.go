package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// The board commands below are the server-mode counterparts of the v1 board
// commands. They keep the v1 arguments; output names the server project
// instead of a local path.

type commandOutput struct{ diagnostic bool }

func (out commandOutput) Write(data []byte) (int, error) {
	if out.diagnostic {
		return os.Stderr.Write(data)
	}
	return os.Stdout.Write(data)
}

var stdout io.Writer = commandOutput{}
var stderr io.Writer = commandOutput{diagnostic: true}

// remark reports a note of the command that is not an error: a line on stderr, or with --json an
// entry of the notices of the result.
func remark(code, text string) {
	if command.JSON() {
		command.Notify(code, text)
		return
	}
	fmt.Fprintln(stderr, text)
}

// fail reports err and ends the command with exit status 1.
func fail(err error) error { return command.Exit(command.Fail(err)) }

func init() { command.RegisterClassifier(classify) }

// classify gives the failures of the service session their stable codes and texts: a board
// rejection by its board code with the service text, another service error by its API code, an
// unreachable service, and a checkout without a project binding.
func classify(err error) (code, message string, ok bool) {
	var e *api.Error
	switch {
	case errors.As(err, &e):
		if e.BoardCode != "" {
			return e.BoardCode, e.Message, true
		}
		return e.Code, config.Text("remote.error_"+e.Code) + ": " + e.Message, true
	case client.IsUnreachable(err):
		return CodeUnreachable, config.Text("remote.unreachable", err.Error()), true
	case errors.Is(err, ErrNotConfigured):
		return CodeNotConfigured, config.Text("remote.not_configured"), true
	}
	return "", "", false
}

// Stable codes of the failures that have no API or board code.
const (
	CodeUnreachable   = "unreachable"
	CodeNotConfigured = "not_configured"
)

func open(ctx context.Context) (*Session, *config.Config, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return nil, nil, err
	}
	dir, _ := os.Getwd()
	s, err := Open(ctx, cfg, dir)
	return s, cfg, err
}

var commandContext = func() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func location(s *Session, task string) string {
	return "kander-server:" + s.Info.Name + "/" + task
}

// NewListCommand builds kander list. observe supplies this host's terminal observer for
// --agent-status; the board package cannot import it.
func NewListCommand(observe board.AgentStatusFunc) *cobra.Command {
	var agentStatus bool
	c := command.RequireProject(&cobra.Command{
		Use:     "list [state]",
		Aliases: []string{"ls"},
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(0, 1)(c, args); err != nil {
				return err
			}
			if len(args) == 1 && !slices.Contains(board.States, args[0]) {
				return command.Usage("board.unknown_state", command.Escape(args[0]))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			state := ""
			if len(args) == 1 {
				state = args[0]
			}
			return runList(state, agentStatus, observe)
		},
	})
	c.PreRunE = func(*cobra.Command, []string) error {
		if agentStatus && !command.JSON() {
			return command.Usage("cli.error_flag_needs", "--agent-status", "--json")
		}
		return nil
	}
	c.Flags().BoolVar(&agentStatus, "agent-status", false, "")
	return c
}

func runList(state string, agentStatus bool, observe board.AgentStatusFunc) error {
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	list, err := s.Project.List(ctx, state)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		if !agentStatus {
			observe = nil
		}
		snapshot, err := s.machineList(ctx, list, observe)
		if err != nil {
			return fail(err)
		}
		return command.Exit(command.Result(snapshot))
	}
	w := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	for _, t := range list.Tasks {
		owner := t.Assignee
		if t.Claim != nil && t.Claim.Status != api.ClaimReleased {
			owner = t.Claim.Owner + "@" + t.Claim.ClientID + " g" + strconv.FormatUint(t.Claim.Generation, 10) + " " + t.Claim.Status
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.State, t.TaskID, t.Title, cmpOr(owner, "-"))
	}
	w.Flush()
	return nil
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// emit writes the object a command always prints, whatever --json says.
func emit(value any) error { return command.Exit(command.Object(value)) }

// NewShowCommand builds kander show.
func NewShowCommand() *cobra.Command {
	var document string
	c := command.RequireProject(&cobra.Command{
		Use:  "show <task-id>",
		Args: command.Args(1, 1),
	})
	c.RunE = func(_ *cobra.Command, args []string) error {
		return runShow(args[0], document, c.Flags().Changed("document"))
	}
	command.Once(c.Flags(), &document, "document", "")
	return c
}

func runShow(task, document string, withDocument bool) error {
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	if withDocument {
		d, err := s.Project.Document(ctx, task, document)
		if err != nil {
			return fail(err)
		}
		if command.JSON() {
			return command.Exit(command.Result(d))
		}
		io.WriteString(stdout, d.Text)
		return nil
	}
	t, err := s.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(t))
	}
	fmt.Fprintln(stdout, config.Text("board.show_location", t.State, location(s, t.TaskID)))
	if t.Claim != nil {
		fmt.Fprintln(stdout, config.Text("remote.claim_line", t.Claim.Owner, t.Claim.ClientID, t.Claim.Generation, t.Claim.Status, t.Claim.ExpiresAt.Local().Format(time.DateTime)))
	}
	fmt.Fprintln(stdout)
	io.WriteString(stdout, t.Text)
	return nil
}

// NewNewCommand builds kander new.
func NewNewCommand() *cobra.Command {
	var large bool
	var language string
	c := command.RequireProject(&cobra.Command{
		Use: "new <feature|bug|chore|research> <slug> <title...>",
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(3, -1)(c, args); err != nil {
				return err
			}
			if !slices.Contains(board.TaskTypes(), args[0]) {
				return command.Usage("board.unknown_task_type", command.Escape(args[0]))
			}
			return nil
		},
	})
	c.RunE = func(_ *cobra.Command, args []string) error {
		return runNew(args[0], args[1], strings.Join(args[2:], " "), large, language, c.Flags().Changed("language"))
	}
	c.Flags().BoolVar(&large, "large", false, "")
	c.Flags().StringVar(&language, "language", "", "")
	return c
}

func runNew(kind, slug, title string, large bool, language string, languageGiven bool) error {
	ctx, cancel := commandContext()
	defer cancel()
	s, cfg, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	if !languageGiven {
		language = cfg.AgentLanguage
	}
	if language, err = config.ValidateAgentLanguage(language); err != nil {
		if languageGiven {
			return command.UsageText(err.Error())
		}
		return fail(err)
	}
	t, err := s.Project.NewTask(ctx, client.NewKey(), api.NewTask{Type: kind, Slug: slug, Title: title, Language: language, Large: large})
	if err != nil {
		return fail(err)
	}
	return printTask(s, t)
}

// claimFor returns the credential this process presents for task, or nil.
func claimFor(s *Session, task string) (*client.Claim, error) {
	c, found, err := s.MutationCredential(task)
	if err != nil || !found {
		return nil, err
	}
	return c.Claim(), nil
}

// NewUpdateCommand builds kander update.
func NewUpdateCommand() *cobra.Command {
	var o updateOptions
	c := command.RequireProject(&cobra.Command{
		Use:  "update <task-id>",
		Args: command.Args(1, 1),
		PreRunE: func(c *cobra.Command, _ []string) error {
			return command.First(
				command.Required(c, "document", "file", "expect-revision"),
				command.Together(c, "dispatch-id", "execution-epoch"),
			)
		},
		RunE: func(_ *cobra.Command, args []string) error { return runUpdate(args[0], o) },
	})
	command.Once(c.Flags(), &o.document, "document", "")
	command.Once(c.Flags(), &o.file, "file", "")
	c.Flags().Uint64Var(&o.revision, "expect-revision", 0, "")
	command.Once(c.Flags(), &o.decisionFile, "contract-decision-file", "")
	command.Once(c.Flags(), &o.dispatchID, "dispatch-id", "")
	c.Flags().Uint64Var(&o.epoch, "execution-epoch", 0, "")
	return c
}

type updateOptions struct {
	document, file, decisionFile, dispatchID string
	revision, epoch                          uint64
}

func runUpdate(task string, o updateOptions) error {
	authorization, authErr := executionAuthorization(o.dispatchID, o.epoch)
	if authErr != nil {
		return command.UsageText(authErr.Error())
	}
	text, err := readUTF8(o.file)
	if err != nil {
		return fail(err)
	}
	decision := ""
	if path := o.decisionFile; path != "" {
		if decision, err = readUTF8(path); err != nil {
			return fail(err)
		}
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
	claim, err := claimFor(s, task)
	if err != nil {
		return fail(err)
	}
	t, err := s.Project.PutDocument(ctx, client.NewKey(), task, api.Document{Path: o.document, Text: text, ExpectedRevision: o.revision, ContractDecision: decision, Authorization: authorization}, claim)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(updateResult{TaskID: t.TaskID, Document: o.document, Revision: t.Revision}))
	}
	fmt.Fprintln(stdout, t.Revision)
	return nil
}

func readUTF8(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New(config.Text("board.task_document_is_not_valid_utf_8", path))
	}
	return string(data), nil
}

// NewMoveCommand builds kander move. A move into working with --owner is a claim; the grant
// is stored as this host's credential.
func NewMoveCommand() *cobra.Command {
	var o moveOptions
	c := command.RequireProject(&cobra.Command{
		Use:  "move <task-id> <state>",
		Args: command.Args(2, 2),
		PreRunE: func(c *cobra.Command, _ []string) error {
			return command.Together(c, "dispatch-id", "execution-epoch")
		},
	})
	c.RunE = func(_ *cobra.Command, args []string) error { return runMove(args[0], args[1], o) }
	flags := c.Flags()
	flags.StringVar(&o.owner, "owner", "", "")
	flags.StringVar(&o.result, "result", "", "")
	flags.StringVar(&o.reason, "reason", "", "")
	flags.StringVar(&o.decision, "decision", "", "")
	flags.StringVar(&o.duplicateOf, "duplicate-of", "", "")
	flags.Uint64Var(&o.revision, "expect-revision", 0, "")
	command.Once(flags, &o.dispatchID, "dispatch-id", "")
	flags.Uint64Var(&o.epoch, "execution-epoch", 0, "")
	flags.StringVar(&o.deliveryCommit, "delivery-commit", "", "")
	flags.StringVar(&o.disposition, "disposition", "", "")
	o.cmd = c
	return c
}

type moveOptions struct {
	cmd                                                      *cobra.Command
	owner, result, reason, decision, duplicateOf, dispatchID string
	deliveryCommit, disposition                              string
	revision, epoch                                          uint64
}

func runMove(task, state string, o moveOptions) error {
	authorization, authErr := executionAuthorization(o.dispatchID, o.epoch)
	if authErr != nil {
		return command.UsageText(authErr.Error())
	}
	if authorization.DispatchID != "" && o.owner != "" {
		return command.Usage("cli.error_flags_exclusive", "--dispatch-id, --owner")
	}

	ctx, cancel := commandContext()
	defer cancel()
	s, cfg, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := s.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	if state == "working" && o.owner != "" {
		return claim(ctx, s, task, o.owner, o.decision)
	}
	req := api.Move{Authorization: authorization, DeliveryCommit: o.deliveryCommit, State: state, Result: o.result, Reason: o.reason, Decision: o.decision, DuplicateOf: o.duplicateOf}
	if path := o.disposition; path != "" {
		id, err := board.NormalizeTaskID(task)
		if err != nil {
			return fail(err)
		}
		req.Disposition = &board.ArtifactReference{TaskID: id, Path: path}
	}
	if o.cmd.Flags().Changed("expect-revision") {
		v := o.revision
		req.ExpectedRevision = &v
	}
	before, err := s.Project.Show(ctx, task)
	if err != nil {
		return fail(err)
	}
	notices := observeServiceMove(ctx, s, before, state)
	t, err := s.MoveTask(ctx, task, req)
	if err != nil {
		return fail(err)
	}
	replayed := t.MoveReceipt != nil && t.MoveReceipt.Replayed
	var report string
	if !replayed {
		report = board.CompletionReportSnapshot(t.TaskID, t.State, t.Text, location(s, t.TaskID), cfg, command.NoticeWriter("rules_embedded_fallback"))
		if report != "" && !command.JSON() {
			io.WriteString(stderr, report)
			report = ""
		}
		for _, notice := range notices {
			var text bytes.Buffer
			notice(&text, t)
			if text.Len() > 0 {
				remark("issue_result_handoff", strings.TrimRight(text.String(), "\n"))
			}
		}
	}
	if authorization.DispatchID != "" {
		if t.MoveReceipt == nil || t.MoveReceipt.Dispatch == nil {
			return fail(&api.Error{Code: api.CodeEvidenceIncomplete, Message: config.Text("remote.dispatch_receipt_missing")})
		}
		return emit(boundMoveResult{MoveReceipt: t.MoveReceipt, CompletionReport: report})
	}
	if command.JSON() {
		return command.Exit(command.Result(moveResult{taskRef: newTaskRef(s, t), Replayed: replayed, CompletionReport: report}))
	}
	return printTask(s, t)
}

// reclaimRefusal refuses a takeover by the execution that already owns the
// claim. A claim that still stands (active or lapsed) needs no takeover; a
// lost or released one cannot be taken again under the same execution ID.
func reclaimRefusal(current *api.ClaimView, execution string) error {
	if current == nil || current.ExecutionID != execution {
		return nil
	}
	key := "remote.execution_cannot_reclaim"
	if api.ClaimStands(current.Status) {
		key = "remote.execution_already_holder"
	}
	return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text(key)}
}

// claim takes a todo task, or with a decision takes over a lost claim.
func claim(ctx context.Context, s *Session, task, owner, decision string) error {
	execution := os.Getenv(EnvExecutionID)
	if execution == "" {
		execution = NewExecutionID()
	}
	req := api.ClaimRequest{ClientID: s.Client.ClientID(), ExecutionID: execution, Owner: owner, Decision: decision}
	var grant api.ClaimGrant
	var err error
	if decision == "" {
		grant, err = s.Project.Claim(ctx, client.NewKey(), task, req)
	} else {
		current, serr := s.Project.Show(ctx, task)
		if serr != nil {
			return fail(serr)
		}
		if current.Claim != nil {
			req.ExpectedGeneration = current.Claim.Generation
		}
		if err := reclaimRefusal(current.Claim, execution); err != nil {
			return fail(err)
		}
		if id := board.MetadataFrom(current.Text, "DISPATCH_ID"); id != "" {
			dispatch, err := s.Project.Dispatch(ctx, current.TaskID, id)
			if err != nil {
				return fail(err)
			}
			mode := "retain"
			if dispatch.State == board.DispatchCompleted {
				mode = "settle-completed"
			}
			req.Dispatch = &board.DispatchTakeover{Authorization: dispatch.Authorization, Revision: dispatch.Revision, Mode: mode}
		}

		grant, err = s.Project.Takeover(ctx, client.NewKey(), task, req)
	}
	if err != nil {
		return fail(err)
	}
	credential := Credential{
		ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: s.Info.ID, TaskID: grant.Task.TaskID,
		ExecutionID: execution, Generation: grant.Claim.Generation, Token: grant.Token,
		RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: time.Now().UTC(),
	}
	if err := s.SaveCredential(credential); err != nil {
		return fail(err)
	}
	if notice, err := EnsureWorkerChecked(s.Home); err != nil {
		remark("worker_start_failed", config.Text("remote.worker_start_failed", err.Error()))
	} else if notice != "" {
		remark("worker_version_warning", notice)
	}
	if command.JSON() {
		return command.Exit(command.Result(claimResult{taskRef: newTaskRef(s, grant.Task), Generation: grant.Claim.Generation, ExecutionID: execution}))
	}
	fmt.Fprintln(stdout, location(s, grant.Task.TaskID))
	fmt.Fprintln(stderr, config.Text("remote.claimed", grant.Task.TaskID, grant.Claim.Generation, execution))
	return nil
}

// NewPickCommand builds kander pick.
func NewPickCommand() *cobra.Command {
	return command.RequireProject(&cobra.Command{
		Use:  "pick [task-id]",
		Args: command.Args(0, 1),
		// Choosing a task from a list needs a terminal; --json refuses it as an argument error.
		PreRunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 && command.JSON() {
				return command.Interactive(config.Text("remote.interactive_select_task"))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error { return runPick(args) },
	})
}

func runPick(args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	session, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	operationCtx, finishOperation := session.OperationContext(ctx)
	defer finishOperation()
	ctx = operationCtx
	var task api.TaskSummary
	if len(args) == 0 {
		task, err = SelectTask(ctx, session, "backlog", os.Stdin, stdout)
	} else {
		var current api.Task
		current, err = session.Project.Show(ctx, args[0])
		task = api.TaskSummary{TaskID: current.TaskID, Revision: current.Revision, State: current.State}
	}
	if err != nil {
		return fail(err)
	}
	revision := task.Revision
	moved, err := session.MoveTask(ctx, task.TaskID, api.Move{State: "todo", ExpectedRevision: &revision})
	if err != nil {
		return fail(err)
	}
	return printTask(session, moved)
}

func executionAuthorization(id string, epoch uint64) (board.ExecutionAuthorization, error) {
	if id == "" && epoch == 0 {
		return board.ExecutionAuthorization{}, nil
	}
	if id == "" || epoch == 0 || !api.ValidIdentity(id) {
		return board.ExecutionAuthorization{}, errors.New("invalid dispatch authorization")
	}
	return board.ExecutionAuthorization{DispatchID: id, Epoch: epoch}, nil
}
