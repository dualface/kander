package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/servicectl"
)

// GCReport is the plan or the outcome of `kander gc`: one entry per project,
// the client project directories that match no project of the bound server,
// the part of the local server gc, and the failures outside any project.
type GCReport struct {
	DryRun    bool        `json:"dry_run"`
	Projects  []GCProject `json:"projects"`
	Unmatched []string    `json:"unmatched,omitempty"`
	Server    GCServer    `json:"server"`
	Errors    []string    `json:"errors,omitempty"`
}

// GCServer is the local server gc as `kander gc` saw it: skipped with the
// reason, refused (HTTP 403 or a gc already running), failed, or its report.
type GCServer struct {
	Skipped string              `json:"skipped,omitempty"`
	Refused string              `json:"refused,omitempty"`
	Error   string              `json:"error,omitempty"`
	Report  *api.ServerGCReport `json:"report,omitempty"`
}

// Incomplete reports a kept candidate, an error, or a refused server part,
// which makes the command exit 1.
func (r GCReport) Incomplete() bool {
	if len(r.Errors) > 0 || r.Server.Refused != "" || r.Server.Error != "" || r.Server.Report != nil && r.Server.Report.Incomplete() {
		return true
	}
	return slices.ContainsFunc(r.Projects, GCProject.Incomplete)
}

// Empty reports a plan with nothing to remove.
func (r GCReport) Empty() bool {
	if s := r.Server.Report; s != nil {
		if s.Log != nil && (s.Log.Bytes > 0 || s.Log.Recovered > 0) {
			return false
		}
		for _, p := range s.Projects {
			if p.Compaction.Runs > 0 || p.Receipts.Files > 0 {
				return false
			}
		}
	}
	for _, p := range r.Projects {
		if !p.Empty() {
			return false
		}
	}
	return true
}

func (r GCReport) status() int {
	if r.Incomplete() {
		return 1
	}
	return 0
}

// serverGC calls the gc of the local server.
type serverGC func(context.Context, api.ServerGCRequest) (api.ServerGCReport, error)

// The prompt streams, the terminal test, the clock, the interrupt and the
// local server lookup of `kander gc`; tests replace them.
var (
	gcPromptIn  io.Reader = os.Stdin
	gcPromptOut io.Writer = os.Stderr
	gcTerminal            = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
	}
	gcNow = time.Now
	// gcInterrupt is the context an interrupt cancels.
	gcInterrupt = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	}
	// gcOpen opens the bound project's session.
	gcOpen = func() (*Session, *config.Config, error) {
		ctx, cancel := commandContext()
		defer cancel()
		return open(ctx)
	}
	// resolveServerGC returns the gc call of the server at target, or the
	// reason it is skipped: the server is not on a loopback address, or this
	// user cannot read its administration key.
	resolveServerGC = func(ctx context.Context, target string) (serverGC, string, error) {
		if !servicectl.Local(target) {
			return nil, config.Text("gc.client_server_remote", target), nil
		}
		info, err := servicectl.Info(ctx, target)
		if err != nil {
			return nil, "", err
		}
		if err := servicectl.CheckAdminToken(info.DataDir); err != nil {
			return nil, config.Text("gc.client_server_no_key", info.DataDir, err.Error()), nil
		}
		return func(ctx context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
			return servicectl.GC(ctx, info, req)
		}, "", nil
	}
)

type clientGCFlags struct {
	olderThan                            string
	dryRun, yes, allProjects, clientOnly bool
}

// NewGCCommand builds `kander gc`. probe classifies the retained executors of
// a worktree; liveness provides it, which this package cannot import.
func NewGCCommand(probe ExecutorProbe) *cobra.Command {
	o := clientGCFlags{olderThan: "7d"}
	c := command.RequireProject(&cobra.Command{
		Use:  "gc",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runClientGC(o, probe, os.Stdout)) },
	})
	flags := c.Flags()
	command.Checked(flags, &o.olderThan, "older-than", "", func(raw string) error {
		_, err := command.ParseRetention("older-than", raw)
		return err
	})
	flags.BoolVar(&o.dryRun, "dry-run", false, "")
	flags.BoolVar(&o.yes, "yes", false, "")
	flags.BoolVar(&o.allProjects, "all-projects", false, "")
	flags.BoolVar(&o.clientOnly, "client-only", false, "")
	return c
}

// clientGC holds what one invocation plans and then runs.
type clientGC struct {
	flags    clientGCFlags
	opts     GCOptions
	sessions []*Session
	checkout string
	server   serverGC
	request  api.ServerGCRequest
}

func runClientGC(o clientGCFlags, probe ExecutorProbe, stdout io.Writer) int {
	if !o.yes && !o.dryRun {
		if command.JSON() {
			return command.FailStatus(2, command.Interactive(config.Text("gc.client_requires_yes")))
		}
		if !gcTerminal() {
			fmt.Fprintln(os.Stderr, config.Text("gc.client_requires_yes"))
			return 2
		}
	}
	olderThan, err := command.ParseRetention("older-than", o.olderThan)
	if err != nil {
		return command.FailStatus(2, err)
	}
	s, cfg, err := gcOpen()
	if err != nil {
		return command.Fail(err)
	}
	ctx, stop := gcInterrupt()
	defer stop()
	g := &clientGC{flags: o, sessions: []*Session{s}, request: api.ServerGCRequest{OlderThan: olderThan.String(), LogKeep: command.DefaultLogKeep}}
	g.checkout, _ = os.Getwd()
	g.opts = GCOptions{OlderThan: olderThan, Probe: probe, Mainline: config.MainlineBranch(cfg)}
	if !o.allProjects {
		g.request.Project = s.Info.Name
	}
	plan := g.pass(ctx, true, nil)
	if o.dryRun {
		return gcClientOutput(stdout, plan)
	}
	// Text output shows the plan before anything changes, with --yes too;
	// JSON output stays one object, the result.
	if !command.JSON() {
		printClientGC(stdout, plan)
		if plan.Empty() {
			return plan.status()
		}
	}
	if !o.yes {
		confirmed, err := askClientGC(ctx)
		if err != nil {
			return command.Fail(err)
		}
		if !confirmed || ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, config.Text("gc.cancelled"))
			return 1
		}
	}
	return gcClientOutput(stdout, g.pass(ctx, false, &plan))
}

// pass plans (dryRun) or runs the gc. The plan asks the server first, then
// selects the projects and plans the client part; a run removes the client
// part first and then runs the server gc, unless the plan already found the
// server part skipped, refused or failed.
func (g *clientGC) pass(ctx context.Context, dryRun bool, plan *GCReport) GCReport {
	report := GCReport{DryRun: dryRun}
	if plan == nil {
		g.planServer(ctx, &report)
		g.selectProjects(ctx, &report)
	} else {
		report.Server = GCServer{Skipped: plan.Server.Skipped, Refused: plan.Server.Refused, Error: plan.Server.Error}
		report.Unmatched, report.Errors = plan.Unmatched, plan.Errors
	}
	opts := g.opts
	opts.DryRun, opts.Now = dryRun, gcNow()
	for i, s := range g.sessions {
		opts.Checkout = ""
		if i == 0 {
			opts.Checkout = g.checkout
		}
		report.Projects = append(report.Projects, s.CollectGarbage(ctx, opts))
	}
	if !dryRun && g.server != nil && plan.Server.Report != nil {
		g.callServer(ctx, &report, false)
	}
	return report
}

func (g *clientGC) planServer(ctx context.Context, report *GCReport) {
	if g.flags.clientOnly {
		report.Server.Skipped = config.Text("gc.client_server_client_only")
		return
	}
	call, skipped, err := resolveServerGC(ctx, g.sessions[0].ServerURL)
	switch {
	case err != nil:
		report.Server.Error = err.Error()
	case call == nil:
		report.Server.Skipped = skipped
	default:
		g.server = call
		g.callServer(ctx, report, true)
	}
}

// callServer sends one server gc request and records its report, or the
// refusal (HTTP 403, a gc already running) or the failure.
func (g *clientGC) callServer(ctx context.Context, report *GCReport, dryRun bool) {
	request := g.request
	request.DryRun = dryRun
	result, err := g.server(ctx, request)
	var refused *api.Error
	switch {
	case err == nil:
		report.Server.Report = &result
	case errors.As(err, &refused) && refused.Status == http.StatusForbidden:
		report.Server.Refused = config.Text("gc.refused")
	case errors.As(err, &refused) && refused.Code == api.CodeGCRunning:
		report.Server.Refused = config.Text("gc.running")
	default:
		report.Server.Error = err.Error()
	}
}

// selectProjects adds, with --all-projects, every project of the bound server
// that has a client project directory here. The names come from the server
// gc plan, the only place that lists every project; without it only the bound
// project can be matched. The other directories are reported unmatched and
// never changed.
func (g *clientGC) selectProjects(ctx context.Context, report *GCReport) {
	if !g.flags.allProjects {
		return
	}
	bound := g.sessions[0]
	root := filepath.Join(bound.Home, "projects")
	entries, err := fs.ListDirectory(bound.Home, root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		report.Errors = append(report.Errors, err.Error())
		return
	}
	local := map[string]bool{}
	for _, entry := range entries {
		if entry.Kind == fs.KindDirectory {
			local[entry.Name] = true
		}
	}
	matched := map[string]bool{projectNamespace(bound.ServerURL, bound.Info.ID): true}
	if s := report.Server.Report; s != nil {
		for _, project := range s.Projects {
			if project.Name == bound.Info.Name || !local[projectNamespace(bound.ServerURL, api.ProjectID(project.Name))] {
				continue
			}
			info, err := bound.Client.ProjectByName(ctx, project.Name)
			if err != nil {
				report.Errors = append(report.Errors, project.Name+": "+err.Error())
				continue
			}
			matched[projectNamespace(bound.ServerURL, info.ID)] = true
			g.sessions = append(g.sessions, &Session{ServerURL: bound.ServerURL, Client: bound.Client, Project: bound.Client.Project(info.ID), Info: info, Home: bound.Home})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(local)) {
		if !matched[name] {
			report.Unmatched = append(report.Unmatched, name)
		}
	}
}

func gcClientOutput(stdout io.Writer, r GCReport) int {
	if command.JSON() {
		return command.ResultStatus(r, r.status())
	}
	printClientGC(stdout, r)
	return r.status()
}

func printClientGC(w io.Writer, r GCReport) {
	header := "gc.result_header"
	if r.DryRun {
		header = "gc.plan_header"
	}
	fmt.Fprintln(w, config.Text(header))
	for _, p := range r.Projects {
		fmt.Fprintln(w, config.Text("gc.client_project_line", p.Name, p.Worktrees.Count, command.FormatBytes(p.Worktrees.Bytes), p.Branches.Count, p.ReviewRuntime.Count, command.FormatBytes(p.ReviewRuntime.Bytes), p.Journal.Count, command.FormatBytes(p.Journal.Bytes), p.EvidenceRefs.Count))
		for _, c := range []struct {
			key      string
			category GCCategory
		}{{"gc.client_kept_worktree", p.Worktrees}, {"gc.client_kept_branch", p.Branches}, {"gc.client_kept_runtime", p.ReviewRuntime}, {"gc.client_kept_journal", p.Journal}, {"gc.client_kept_ref", p.EvidenceRefs}} {
			for _, kept := range c.category.Kept {
				fmt.Fprintln(w, config.Text(c.key, kept.ID, oneLine(kept.Reason)))
			}
		}
		for _, deferred := range p.Worktrees.Deferred {
			fmt.Fprintln(w, config.Text("gc.client_deferred", deferred.ID, oneLine(deferred.Reason)))
		}
		for _, skipped := range p.Skipped {
			fmt.Fprintln(w, config.Text("gc.client_skipped", oneLine(skipped)))
		}
		for _, e := range p.Errors {
			fmt.Fprintln(w, config.Text("gc.error", oneLine(e)))
		}
	}
	if len(r.Unmatched) > 0 {
		fmt.Fprintln(w, config.Text("gc.client_unmatched"))
		for _, name := range r.Unmatched {
			fmt.Fprintln(w, "  "+name)
		}
	}
	printServerPart(w, r.Server)
	for _, e := range r.Errors {
		fmt.Fprintln(w, config.Text("gc.error", oneLine(e)))
	}
	if r.Empty() && !r.Incomplete() {
		fmt.Fprintln(w, config.Text("gc.nothing"))
	}
}

// printServerPart prints the server part with the lines of `kander server gc`.
func printServerPart(w io.Writer, s GCServer) {
	switch {
	case s.Skipped != "":
		fmt.Fprintln(w, config.Text("gc.client_server_skipped", oneLine(s.Skipped)))
	case s.Refused != "":
		fmt.Fprintln(w, config.Text("gc.client_server_refused", oneLine(s.Refused)))
	case s.Error != "":
		fmt.Fprintln(w, config.Text("gc.client_server_error", oneLine(s.Error)))
	}
	if s.Report == nil {
		return
	}
	fmt.Fprintln(w, config.Text("gc.client_server_header"))
	for _, p := range s.Report.Projects {
		fmt.Fprintln(w, config.Text("gc.project_line", p.Name, p.Compaction.Runs, p.Compaction.Files, command.FormatBytes(p.Compaction.Bytes), p.Receipts.Files, command.FormatBytes(p.Receipts.Bytes)))
		for _, kept := range p.Compaction.Kept {
			fmt.Fprintln(w, config.Text("gc.kept_run", kept.ID, oneLine(kept.Reason)))
		}
		for _, kept := range p.Receipts.Kept {
			fmt.Fprintln(w, config.Text("gc.kept_receipt", kept.ID, oneLine(kept.Reason)))
		}
		for _, warning := range p.Warnings {
			fmt.Fprintln(w, config.Text("gc.warning", oneLine(warning)))
		}
		for _, e := range p.Errors {
			fmt.Fprintln(w, config.Text("gc.error", oneLine(e)))
		}
	}
	if s.Report.Log != nil {
		fmt.Fprintln(w, config.Text("gc.log_line", s.Report.Log.Path, command.FormatBytes(s.Report.Log.Size), command.FormatBytes(s.Report.Log.Bytes)))
		if s.Report.Log.Recovered > 0 {
			key := "gc.log_recovered"
			if s.Report.DryRun {
				key = "gc.log_recover"
			}
			fmt.Fprintln(w, config.Text(key, command.FormatBytes(s.Report.Log.Recovered)))
		}
		if s.Report.Log.Kept != "" {
			fmt.Fprintln(w, config.Text("gc.kept_log", oneLine(s.Report.Log.Kept)))
		}
	}
	for _, e := range s.Report.Errors {
		fmt.Fprintln(w, config.Text("gc.error", oneLine(e)))
	}
}

// oneLine keeps a reason on one visible line.
func oneLine(text string) string { return command.Escape(strings.TrimSpace(text)) }

// askClientGC asks y/N on stderr; anything but y or yes, including EOF, is
// no. An interrupt while it waits is no as well: the read is left behind and
// the command exits without removing anything.
func askClientGC(ctx context.Context) (bool, error) {
	fmt.Fprint(gcPromptOut, config.Text("gc.question"))
	type answer struct {
		line string
		err  error
	}
	read, in := make(chan answer, 1), gcPromptIn
	go func() {
		line, err := bufio.NewReader(in).ReadString('\n')
		read <- answer{line, err}
	}()
	var a answer
	select {
	case <-ctx.Done():
		fmt.Fprintln(gcPromptOut)
		return false, nil
	case a = <-read:
	}
	if a.err != nil && strings.TrimSpace(a.line) == "" {
		if errors.Is(a.err, io.EOF) {
			return false, nil
		}
		return false, a.err
	}
	reply := strings.ToLower(strings.TrimSpace(a.line))
	return reply == "y" || reply == "yes", nil
}
