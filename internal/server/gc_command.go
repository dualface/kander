package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/internal/version"
)

// gcFlags are the flags of `kander server gc`.
type gcFlags struct {
	data, project, olderThan, logKeep string
	dryRun, yes                       bool
}

// The prompt streams and the terminal test of `kander server gc`; tests replace them.
var (
	gcPromptIn  io.Reader = os.Stdin
	gcPromptOut io.Writer = os.Stderr
	gcTerminal            = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
	}
	// gcServerURL returns the configured server URL, like importTarget.
	gcServerURL = func() (string, error) { return importTarget() }
	// gcProbe finds the loopback server at a URL that serves a data directory.
	gcProbe = servicectl.ServerFor
	// gcInterrupt is the context an interrupt cancels.
	gcInterrupt = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	}
)

// NewGCCommand builds `kander server gc`. It plans, asks, and then reclaims
// server data: through the running server's administration route, or offline
// under the data directory lock when no server runs.
func NewGCCommand() *cobra.Command {
	o := gcFlags{olderThan: "7d", logKeep: "64MiB"}
	c := &cobra.Command{
		Use:  "gc",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runGC(o, os.Stdout)) },
	}
	flags := c.Flags()
	flags.StringVar(&o.data, "data", "", "")
	command.Checked(flags, &o.project, "project", "", func(raw string) error {
		if !api.ValidProjectName(raw) {
			return command.Usage("gc.invalid_project", command.Escape(raw), api.ProjectNameRule)
		}
		return nil
	})
	command.Checked(flags, &o.olderThan, "older-than", "", func(raw string) error {
		_, err := command.ParseRetention("older-than", raw)
		return err
	})
	command.Checked(flags, &o.logKeep, "log-keep", "", func(raw string) error {
		_, err := command.ParseByteSize("log-keep", raw)
		return err
	})
	flags.BoolVar(&o.dryRun, "dry-run", false, "")
	flags.BoolVar(&o.yes, "yes", false, "")
	return c
}

// gcExecutor plans (dryRun) or runs one gc.
type gcExecutor func(ctx context.Context, dryRun bool) (api.ServerGCReport, error)

func runGC(o gcFlags, stdout io.Writer) int {
	if !o.yes && !o.dryRun {
		if command.JSON() {
			return command.FailStatus(2, command.Interactive(config.Text("gc.requires_yes")))
		}
		if !gcTerminal() {
			fmt.Fprintln(os.Stderr, config.Text("gc.requires_yes"))
			return 2
		}
	}
	olderThan, err := command.ParseRetention("older-than", o.olderThan)
	if err != nil {
		return command.FailStatus(2, err)
	}
	logKeep, err := command.ParseByteSize("log-keep", o.logKeep)
	if err != nil {
		return command.FailStatus(2, err)
	}
	dir, err := dataDir(o.data)
	if err != nil {
		return command.Fail(err)
	}
	ctx, stop := gcInterrupt()
	defer stop()
	request := api.ServerGCRequest{Project: o.project, OlderThan: olderThan.String(), LogKeep: logKeep}
	execute, closeTarget, err := gcExecutorFor(ctx, dir, request)
	if err != nil {
		return gcFailure(err)
	}
	defer func() {
		if err := closeTarget(); err != nil {
			command.Notify("gc_close", err.Error())
		}
	}()
	plan, err := execute(ctx, true)
	if err != nil {
		return gcFailure(err)
	}
	if o.dryRun {
		return gcOutput(stdout, plan)
	}
	// Text output shows the plan before anything changes, with --yes too;
	// JSON output stays one object, the result.
	if !command.JSON() {
		printGCReport(stdout, plan)
		if gcEmpty(plan) {
			return gcStatus(plan)
		}
	}
	if !o.yes {
		confirmed, err := askGC(ctx)
		if err != nil {
			return command.Fail(err)
		}
		if !confirmed || ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, config.Text("gc.cancelled"))
			return 1
		}
	}
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, config.Text("gc.cancelled"))
		return 1
	}
	result, err := execute(ctx, false)
	if err != nil {
		return gcFailure(err)
	}
	return gcOutput(stdout, result)
}

// gcExecutorFor picks where the gc runs: the loopback server that serves dir,
// else this process under the data directory lock. A lock that turns out to
// be held means a server is starting or running, so the server is asked again.
func gcExecutorFor(ctx context.Context, dir string, request api.ServerGCRequest) (gcExecutor, func() error, error) {
	target, err := gcServerURL()
	if err != nil {
		return nil, nil, err
	}
	online := func(info api.ServerInfo) (gcExecutor, func() error, error) {
		return func(ctx context.Context, dryRun bool) (api.ServerGCReport, error) {
			r := request
			r.DryRun = dryRun
			return servicectl.GC(ctx, info, r)
		}, func() error { return nil }, nil
	}
	local := servicectl.Local(target)
	if local {
		if info, err := gcProbe(ctx, target, dir); err == nil {
			return online(info)
		}
	}
	offline, err := OpenOfflineGC(dir, version.String())
	if err == nil {
		return func(ctx context.Context, dryRun bool) (api.ServerGCReport, error) {
			r := request
			r.DryRun = dryRun
			return offline.Run(ctx, r)
		}, offline.Close, nil
	}
	if !errors.Is(err, ErrDataDirInUse) {
		return nil, nil, err
	}
	// A server holds the lock. Ask the configured URL again, since it may have
	// finished starting, then the address the server of dir recorded, which
	// differs when it was started with another --listen.
	var candidates []string
	if local {
		candidates = append(candidates, target)
	}
	if recorded, err := servicectl.RecordedURL(dir); err == nil && recorded != "" && recorded != target {
		candidates = append(candidates, recorded)
	}
	if len(candidates) == 0 {
		return nil, nil, errors.New(config.Text("gc.local_only", target))
	}
	var last error
	for _, candidate := range candidates {
		info, err := gcProbe(ctx, candidate, dir)
		if err == nil {
			return online(info)
		}
		last = err
	}
	if errors.Is(last, servicectl.ErrNoServer) {
		return nil, nil, errors.New(config.Text("gc.no_server", dir, target))
	}
	return nil, nil, last
}

// gcFailure reports a failed plan or gc. A refused request (bad value, unknown
// project, failed administration check, a gc already running) exits 2; any
// other failure exits 1.
func gcFailure(err error) int {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusForbidden || apiErr.Code == api.CodeGCRunning || apiErr.Code == api.CodeInvalidRequest || apiErr.Code == api.CodeNotFound) {
		message := apiErr.Message
		switch {
		case apiErr.Status == http.StatusForbidden:
			message = config.Text("gc.refused")
		case apiErr.Code == api.CodeGCRunning:
			message = config.Text("gc.running")
		}
		return command.FailStatus(2, &command.Failure{Code: apiErr.Code, Message: message, Err: err})
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return command.Fail(command.Coded("gc_timeout", errors.New(config.Text("gc.timeout"))))
	}
	return command.Fail(err)
}

// gcStatus is 1 when a candidate was kept or an error happened, else 0.
func gcStatus(r api.ServerGCReport) int {
	if r.Incomplete() {
		return 1
	}
	return 0
}

// gcEmpty reports a plan with nothing to reclaim or recover.
func gcEmpty(r api.ServerGCReport) bool {
	if r.Log != nil && (r.Log.Bytes > 0 || r.Log.Recovered > 0) {
		return false
	}
	for _, p := range r.Projects {
		if p.Compaction.Runs > 0 || p.Receipts.Files > 0 {
			return false
		}
	}
	return true
}

func gcOutput(stdout io.Writer, r api.ServerGCReport) int {
	if command.JSON() {
		return command.ResultStatus(r, gcStatus(r))
	}
	printGCReport(stdout, r)
	return gcStatus(r)
}

func printGCReport(w io.Writer, r api.ServerGCReport) {
	header := "gc.result_header"
	if r.DryRun {
		header = "gc.plan_header"
	}
	fmt.Fprintln(w, config.Text(header))
	for _, p := range r.Projects {
		fmt.Fprintln(w, config.Text("gc.project_line", p.Name, p.Compaction.Runs, p.Compaction.Files, formatBytes(p.Compaction.Bytes), p.Receipts.Files, formatBytes(p.Receipts.Bytes)))
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
	if r.Log != nil {
		fmt.Fprintln(w, config.Text("gc.log_line", r.Log.Path, formatBytes(r.Log.Size), formatBytes(r.Log.Bytes)))
		if r.Log.Recovered > 0 {
			key := "gc.log_recovered"
			if r.DryRun {
				key = "gc.log_recover"
			}
			fmt.Fprintln(w, config.Text(key, formatBytes(r.Log.Recovered)))
		}
		if r.Log.Kept != "" {
			fmt.Fprintln(w, config.Text("gc.kept_log", oneLine(r.Log.Kept)))
		}
	}
	for _, e := range r.Errors {
		fmt.Fprintln(w, config.Text("gc.error", oneLine(e)))
	}
	if gcEmpty(r) && !r.Incomplete() {
		fmt.Fprintln(w, config.Text("gc.nothing"))
	}
}

// oneLine keeps a server-provided reason on one visible line.
func oneLine(text string) string { return command.Escape(strings.TrimSpace(text)) }

// formatBytes renders a size with a binary unit.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "B"
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, s
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}

// askGC asks y/N on stderr; anything but y or yes, including EOF, is no. An
// interrupt while it waits is no as well: the read is left behind, and the
// caller exits instead of holding the data directory lock.
func askGC(ctx context.Context) (bool, error) {
	fmt.Fprint(gcPromptOut, config.Text("gc.question"))
	type answer struct {
		line string
		err  error
	}
	read := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(gcPromptIn).ReadString('\n')
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
