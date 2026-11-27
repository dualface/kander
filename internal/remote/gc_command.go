package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
// those whose project could not be confirmed, the part of the local server
// gc, and the failures outside any project. Skipped says why nothing ran.
type GCReport struct {
	DryRun     bool        `json:"dry_run"`
	Skipped    string      `json:"skipped,omitempty"`
	Projects   []GCProject `json:"projects"`
	Unmatched  []string    `json:"unmatched,omitempty"`
	Unverified []GCEntry   `json:"unverified,omitempty"`
	Server     GCServer    `json:"server"`
	Errors     []string    `json:"errors,omitempty"`
}

// GCServer is the local server gc as `kander gc` saw it: skipped with the
// reason, refused (HTTP 403 or a gc already running), failed, or its report.
type GCServer struct {
	Skipped string              `json:"skipped,omitempty"`
	Refused string              `json:"refused,omitempty"`
	Error   string              `json:"error,omitempty"`
	Report  *api.ServerGCReport `json:"report,omitempty"`
}

// Incomplete reports a kept candidate, an error, an unverified client project
// directory, or a refused server part, which makes the command exit 1.
func (r GCReport) Incomplete() bool {
	if len(r.Errors) > 0 || len(r.Unverified) > 0 || r.Server.Refused != "" || r.Server.Error != "" || r.Server.Report != nil && r.Server.Report.Incomplete() {
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
	// gcOpen opens the bound project's session without changing anything.
	gcOpen = func(ctx context.Context) (*Session, *config.Config, error) {
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return openGC(ctx)
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
	ctx, stop := gcInterrupt()
	defer stop()
	s, cfg, err := gcOpen(ctx)
	if errors.Is(err, errNoClientHome) {
		return gcClientOutput(stdout, GCReport{DryRun: true, Skipped: config.Text("gc.client_no_home")})
	}
	if err != nil {
		return command.Fail(err)
	}
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
		report.Unmatched, report.Unverified, report.Errors = plan.Unmatched, plan.Unverified, plan.Errors
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

// selectProjects adds, with --all-projects, every client project directory of
// this host whose project the bound server confirms. The directory's own
// records name its project ID, and a read of that project, which the server's
// access control governs, confirms it. A directory whose records name a
// project of another server, or a project the bound server does not have, is
// unmatched; one that names no project or whose project cannot be read is
// unverified. Neither is changed.
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
	own := projectNamespace(bound.ServerURL, bound.Info.ID)
	var names []string
	for _, entry := range entries {
		if entry.Kind == fs.KindDirectory && entry.Name != own {
			names = append(names, entry.Name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		ids, foreign, err := directoryProjects(bound.Home, bound.ServerURL, name)
		switch {
		case err != nil:
			report.Unverified = append(report.Unverified, GCEntry{ID: name, Reason: err.Error()})
			continue
		case len(ids) != 1 && foreign:
			report.Unmatched = append(report.Unmatched, name)
			continue
		case len(ids) != 1:
			report.Unverified = append(report.Unverified, GCEntry{ID: name, Reason: config.Text("gc.client_directory_unidentified")})
			continue
		}
		info, err := bound.Client.Project(ids[0]).Info(ctx)
		switch {
		case api.IsCode(err, api.CodeNotFound):
			report.Unmatched = append(report.Unmatched, name)
			continue
		case err != nil:
			report.Unverified = append(report.Unverified, GCEntry{ID: name, Reason: err.Error()})
			continue
		case info.ID != ids[0]:
			report.Unverified = append(report.Unverified, GCEntry{ID: name, Reason: config.Text("gc.client_directory_unidentified")})
			continue
		}
		g.sessions = append(g.sessions, &Session{ServerURL: bound.ServerURL, Client: bound.Client, Project: bound.Client.Project(info.ID), Info: info, Home: bound.Home})
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
	if r.Skipped != "" {
		fmt.Fprintln(w, config.Text("gc.client_skipped", oneLine(r.Skipped)))
	}
	for _, p := range r.Projects {
		fmt.Fprintln(w, config.Text("gc.client_project_line", p.Name, p.Worktrees.Count, command.FormatBytes(p.Worktrees.Bytes), p.Branches.Count, p.ReviewRuntime.Count, command.FormatBytes(p.ReviewRuntime.Bytes), p.Journal.Count, command.FormatBytes(p.Journal.Bytes), p.EvidenceRefs.Count, p.TrackingRefs.Count))
		for _, c := range []struct {
			key      string
			category GCCategory
		}{{"gc.client_kept_worktree", p.Worktrees}, {"gc.client_kept_branch", p.Branches}, {"gc.client_kept_runtime", p.ReviewRuntime}, {"gc.client_kept_journal", p.Journal}, {"gc.client_kept_ref", p.EvidenceRefs}, {"gc.client_kept_tracking", p.TrackingRefs}} {
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
	if len(r.Unverified) > 0 {
		fmt.Fprintln(w, config.Text("gc.client_unverified"))
		for _, entry := range r.Unverified {
			fmt.Fprintln(w, "  "+entry.ID+": "+oneLine(entry.Reason))
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
