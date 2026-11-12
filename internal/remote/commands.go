package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
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

func fail(err error) int {
	fmt.Fprintf(stderr, "kander: %s\n", describe(err))
	return 1
}

// describe renders a server error with the v1 localized text when the board
// rejected the operation.
func describe(err error) string {
	var e *api.Error
	if errors.As(err, &e) {
		if e.BoardCode != "" {
			return e.Message
		}
		return config.Text("remote.error_"+e.Code) + ": " + e.Message
	}
	if client.IsUnreachable(err) {
		return config.Text("remote.unreachable", err.Error())
	}
	if errors.Is(err, ErrNotConfigured) {
		return config.Text("remote.not_configured")
	}
	return err.Error()
}

func printUsage(cmd string) int {
	fmt.Fprintln(stderr, config.CommandUsage(cmd))
	return 2
}

func takeValue(args []string, name string) ([]string, string, bool, error) {
	rest := make([]string, 0, len(args))
	value, found := "", false
	for i := 0; i < len(args); i++ {
		if args[i] != name {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) {
			return nil, "", true, errors.New(name)
		}
		value, found = args[i+1], true
		i++
	}
	return rest, value, found, nil
}

func takeFlag(args []string, name string) ([]string, bool) {
	rest := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		rest = append(rest, arg)
	}
	return rest, found
}

func open(ctx context.Context) (*Session, *config.Config, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return nil, nil, err
	}
	dir, _ := os.Getwd()
	s, err := Open(ctx, cfg, dir)
	return s, cfg, err
}

func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func location(s *Session, task string) string {
	return "kander-server:" + s.Info.Name + "/" + task
}

// RunList implements kander list in server mode.
func RunList(args []string) int { return RunListWithAgentStatus(args, nil) }

// RunListWithAgentStatus supplies this host's terminal observer to a REST list.
func RunListWithAgentStatus(args []string, observe board.AgentStatusFunc) int {
	args, machine := takeFlag(args, "--json")
	args, _ = takeFlag(args, "--mobile")
	args, agentStatus := takeFlag(args, "--agent-status")
	if len(args) > 1 || len(args) == 1 && strings.HasPrefix(args[0], "-") {
		return printUsage("list")
	}
	state := ""
	if len(args) == 1 {
		state = args[0]
	}
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	list, err := s.Project.List(ctx, state)
	if err != nil {
		return fail(err)
	}
	if machine {
		if !agentStatus {
			observe = nil
		}
		snapshot, err := s.machineList(ctx, list, observe)
		if err != nil {
			return fail(err)
		}
		return encode(snapshot)
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
	return 0
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func encode(value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		return fail(err)
	}
	return 0
}

// RunShow implements kander show in server mode.
func RunShow(args []string) int {
	args, machine := takeFlag(args, "--json")
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return printUsage("show")
	}
	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	t, err := s.Project.Show(ctx, args[0])
	if err != nil {
		return fail(err)
	}
	if machine {
		return encode(t)
	}
	fmt.Fprintln(stdout, config.Text("board.show_location", t.State, location(s, t.TaskID)))
	if t.Claim != nil {
		fmt.Fprintln(stdout, config.Text("remote.claim_line", t.Claim.Owner, t.Claim.ClientID, t.Claim.Generation, t.Claim.Status, t.Claim.ExpiresAt.Local().Format(time.DateTime)))
	}
	fmt.Fprintln(stdout)
	io.WriteString(stdout, t.Text)
	return 0
}

// RunNew implements kander new in server mode.
func RunNew(args []string) int {
	args, large := takeFlag(args, "--large")
	args, language, languageGiven, err := takeValue(args, "--language")
	if err != nil {
		return printUsage("new")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return printUsage("new")
		}
	}
	if len(args) < 3 {
		return printUsage("new")
	}
	ctx, cancel := commandContext()
	defer cancel()
	s, cfg, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	if !languageGiven {
		language = cfg.AgentLanguage
	}
	if language, err = config.ValidateAgentLanguage(language); err != nil {
		if languageGiven {
			return printUsage("new")
		}
		return fail(err)
	}
	t, err := s.Project.NewTask(ctx, client.NewKey(), api.NewTask{Type: args[0], Slug: args[1], Title: strings.Join(args[2:], " "), Language: language, Large: large})
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, location(s, t.TaskID))
	return 0
}

// claimFor returns the credential this process presents for task, or nil.
func claimFor(s *Session, task string) (*client.Claim, error) {
	c, found, err := s.Credential(task)
	if err != nil || !found {
		return nil, err
	}
	return c.Claim(), nil
}

// RunUpdate implements kander update in server mode.
func RunUpdate(args []string) int {
	values := map[string]string{}
	for _, name := range []string{"--document", "--file", "--expect-revision", "--contract-decision-file", "--dispatch-id", "--execution-epoch"} {
		var err error
		if args, values[name], _, err = takeValue(args, name); err != nil {
			return printUsage("update")
		}
	}
	if len(args) != 1 || values["--document"] == "" || values["--file"] == "" || values["--expect-revision"] == "" {
		return printUsage("update")
	}
	authorization, authErr := executionAuthorization(values)
	if authErr != nil {
		return printUsage("update")
	}
	revision, err := strconv.ParseUint(values["--expect-revision"], 10, 64)
	if err != nil {
		return printUsage("update")
	}
	text, err := readUTF8(values["--file"])
	if err != nil {
		return fail(err)
	}
	decision := ""
	if path := values["--contract-decision-file"]; path != "" {
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
	claim, err := claimFor(s, args[0])
	if err != nil {
		return fail(err)
	}
	t, err := s.Project.PutDocument(ctx, client.NewKey(), args[0], api.Document{Path: values["--document"], Text: text, ExpectedRevision: revision, ContractDecision: decision, Authorization: authorization}, claim)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, t.Revision)
	return 0
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

// RunMove implements kander move in server mode. A move into working with
// --owner is a claim; the grant is stored as this host's credential.
func RunMove(args []string) int {
	values := map[string]string{}
	for _, name := range []string{"--owner", "--result", "--reason", "--decision", "--duplicate-of", "--expect-revision", "--dispatch-id", "--execution-epoch", "--delivery-commit", "--disposition"} {
		var err error
		if args, values[name], _, err = takeValue(args, name); err != nil {
			return printUsage("move")
		}
	}
	if len(args) != 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return printUsage("move")
	}
	authorization, authErr := executionAuthorization(values)
	if authErr != nil || authorization.DispatchID != "" && values["--owner"] != "" {
		return printUsage("move")
	}

	ctx, cancel := commandContext()
	defer cancel()
	s, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
	task, state := args[0], args[1]
	if state == "working" && values["--owner"] != "" {
		return claim(ctx, s, task, values["--owner"], values["--decision"])
	}
	req := api.Move{Authorization: authorization, DeliveryCommit: values["--delivery-commit"], State: state, Result: values["--result"], Reason: values["--reason"], Decision: values["--decision"], DuplicateOf: values["--duplicate-of"]}
	if path := values["--disposition"]; path != "" {
		id, err := board.NormalizeTaskID(task)
		if err != nil {
			return fail(err)
		}
		req.Disposition = &board.ArtifactReference{TaskID: id, Path: path}
	}
	if raw := values["--expect-revision"]; raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return printUsage("move")
		}
		req.ExpectedRevision = &v
	}
	t, err := s.MoveTask(ctx, task, req)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, location(s, t.TaskID))
	return 0
}

// claim takes a todo task, or with a decision takes over a lost claim.
func claim(ctx context.Context, s *Session, task, owner, decision string) int {
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
		if current.Claim != nil && current.Claim.ExecutionID == execution {
			return fail(&api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.execution_cannot_reclaim")})
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
	if err := EnsureWorker(s.Home); err != nil {
		fmt.Fprintln(stderr, config.Text("remote.worker_start_failed", err.Error()))
	}
	fmt.Fprintln(stdout, location(s, grant.Task.TaskID))
	fmt.Fprintln(stderr, config.Text("remote.claimed", grant.Task.TaskID, grant.Claim.Generation, execution))
	return 0
}

// RunPick implements kander pick in server mode.
func RunPick(args []string) int {
	if len(args) > 1 || len(args) == 1 && strings.HasPrefix(args[0], "-") {
		return printUsage("pick")
	}
	ctx, cancel := commandContext()
	defer cancel()
	session, _, err := open(ctx)
	if err != nil {
		return fail(err)
	}
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
	fmt.Fprintln(stdout, location(session, moved.TaskID))
	return 0
}

func executionAuthorization(values map[string]string) (board.ExecutionAuthorization, error) {
	id, raw := values["--dispatch-id"], values["--execution-epoch"]
	if id == "" && raw == "" {
		return board.ExecutionAuthorization{}, nil
	}
	epoch, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == "" || epoch == 0 || !api.ValidIdentity(id) {
		return board.ExecutionAuthorization{}, errors.New("invalid dispatch authorization")
	}
	return board.ExecutionAuthorization{DispatchID: id, Epoch: epoch}, nil
}
