package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/internal/version"
)

// DefaultListen binds loopback; serving a LAN is an explicit choice because
// the first version has no authentication.
const DefaultListen = api.DefaultListen

// RunForeground serves until cancellation, a signal, or authenticated shutdown.
func RunForeground(ctx context.Context, options Options, listen string) error {
	s, err := Open(options)
	if err != nil {
		return err
	}
	defer s.Close()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	s.listen = listener.Addr().String()
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-ctx.Done():
		case <-s.shutdown.Done():
		}
		s.shutdownCancel()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			s.log.Error("shutdown failed", "error", err)
			_ = httpServer.Close()
		}
	}()
	s.log.Info("listening", "address", s.listen, "version", options.Version)
	if options.Ready != nil {
		options.Ready(s.listen)
	}
	err = httpServer.Serve(listener)
	stop()
	<-finished
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func dataDir(value string) (string, error) { return servicectl.DataDir(value) }

// NewImportCommand builds `kander server import`. A remote target receives a
// board upload. A local target uses offline import or its administration route.
// Without --from the source is the board v1 commands locate.
func NewImportCommand() *cobra.Command {
	var o importOptions
	c := &cobra.Command{
		Use:  "import",
		Args: command.Args(0, 0),
		RunE: func(c *cobra.Command, _ []string) error {
			target, err := importTarget()
			if err != nil {
				return err
			}
			if !servicectl.Local(target) && c.Flags().Changed("data") {
				return command.Usage("server.import_upload_data")
			}
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			defer signal.Stop(signals)
			return command.Exit(importCommand(o, target, os.Stdout, os.Stderr, term.IsTerminal(int(os.Stderr.Fd())), signals))
		},
	}
	c.PreRunE = func(*cobra.Command, []string) error { return command.Required(c, "project") }
	c.Flags().StringVar(&o.data, "data", "", "")
	c.Flags().StringVar(&o.name, "project", "", "")
	c.Flags().StringVar(&o.remote, "remote", "", "")
	c.Flags().StringVar(&o.from, "from", "", "")
	c.Flags().BoolVar(&o.skipConflicts, "skip-conflicts", false, "")
	return c
}

// importOptions are the flags of `kander server import`.
type importOptions struct {
	data, name, remote, from string
	skipConflicts            bool
}

// importCommand runs the import with its streams and interrupt source given. A receive on
// signals is one Ctrl+C.
func importCommand(o importOptions, target string, stdout, stderr io.Writer, tty bool, signals chan os.Signal) int {
	// The log lines share stderr with the status line, so they go through the reporter. With
	// --json there is no progress and no log: the result or the error object says it all.
	if command.JSON() {
		stderr = io.Discard
	}
	report := newImportReporter(stderr, tty)
	logger := slog.New(slog.NewJSONHandler(report, nil))
	fail := func(err error) int {
		if command.JSON() {
			return command.Fail(err)
		}
		return 1
	}
	upload := !servicectl.Local(target)
	var dir string
	if !upload {
		var err error
		dir, err = dataDir(o.data)
		if err != nil {
			logger.Error("data directory", "error", err)
			return fail(err)
		}
	}
	source, err := importSource(o.from)
	if err != nil {
		logger.Error("source board", "error", err)
		return fail(err)
	}
	interrupt := watchImportInterrupt(report, signals)
	defer interrupt.stop()
	request := api.ImportRequest{Project: o.name, Remote: o.remote, From: source, SkipConflicts: o.skipConflicts}
	run := importRun{logger: logger, report: report.Report, offline: &interrupt.offline, uploadSent: &interrupt.uploadSent}
	var result api.ImportResult
	var online bool
	if upload {
		result, err = uploadImport(interrupt.ctx, target, request, run)
	} else {
		result, online, err = importBoard(interrupt.ctx, target, dir, request, run)
	}
	report.Finish()
	if err != nil {
		if interrupt.cancelled() {
			key := "server.import_cancelled"
			if (upload && interrupt.uploadSent.Load()) || (online && (report.PastCopy() || !report.Seen())) {
				key = "server.import_cancelled_late"
			}
			report.Notice(config.Text(key))
			if command.JSON() {
				return command.FailStatus(130, command.Coded("cancelled", errors.New(config.Text(key))))
			}
			return 130
		}
		logger.Error("import failed", "error", err)
		return fail(err)
	}
	if command.JSON() {
		return command.Result(result)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(stdout, string(out))
	return 0
}

// importInterrupt turns the first Ctrl+C into a cancellation of the import
// context, and gives a second Ctrl+C its default effect: the process ends. An
// offline import that has copied everything does not cancel, because
// validation and publication do not observe cancellation and must finish.
type importInterrupt struct {
	ctx        context.Context
	signals    chan os.Signal
	cancel     context.CancelFunc
	offline    atomic.Bool
	uploadSent atomic.Bool
	hit        atomic.Bool
}

func watchImportInterrupt(report *importReporter, signals chan os.Signal) *importInterrupt {
	ctx, cancel := context.WithCancel(context.Background())
	w := &importInterrupt{ctx: ctx, cancel: cancel, signals: signals}
	go func() {
		select {
		case <-signals:
		case <-ctx.Done():
			return
		}
		// Stopping the delivery restores the default effect of the signal.
		signal.Stop(signals)
		switch {
		case w.offline.Load() && report.PastCopy():
			report.Notice(config.Text("server.import_interrupt_finishing"))
		case w.offline.Load():
			report.Notice(config.Text("server.import_interrupt_cancelling"))
			w.hit.Store(true)
			cancel()
		default:
			w.hit.Store(true)
			cancel()
		}
	}()
	return w
}

// cancelled reports whether an interrupt cancelled the import.
func (w *importInterrupt) cancelled() bool { return w.hit.Load() }

func (w *importInterrupt) stop() { w.cancel() }

// importSource resolves --from, or the v1 board location when it is omitted,
// to an existing directory. Links are refused later by the board reads.
func importSource(from string) (string, error) {
	var path string
	var err error
	if from != "" {
		path, err = filepath.Abs(from)
	} else {
		path, err = board.BoardRoot()
	}
	if err != nil {
		return "", errors.New(config.Text("server.import_source_missing", err.Error()))
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", errors.New(config.Text("server.import_source_missing", path))
	}
	return path, nil
}

// importTarget returns the configured server URL; tests replace it.
var importTarget = func() (string, error) {
	cfg, err := config.Load(true)
	if err != nil {
		return "", err
	}
	return config.ServerURL(cfg), nil
}

// importRun carries what an import reports to. offline is set once the import
// runs in this process, which decides how an interrupt is handled.
type importRun struct {
	logger     *slog.Logger
	report     func(api.ImportProgress)
	offline    *atomic.Bool
	uploadSent *atomic.Bool
}

// importOpen opens the data directory for an offline import; tests replace it.
var importOpen = Open

// importBoard imports offline under the data directory lock, or through the
// running server that holds it, and says which one it used.
func importBoard(ctx context.Context, target, dir string, request api.ImportRequest, run importRun) (result api.ImportResult, online bool, err error) {
	s, err := importOpen(Options{DataDir: dir, Logger: run.logger, Version: version.String()})
	if err == nil {
		defer s.Close()
		if run.offline != nil {
			run.offline.Store(true)
		}
		ctx, cancel := context.WithTimeout(ctx, ImportTimeout)
		defer cancel()
		result, err = s.importV1Board(ctx, request, run.report)
		return result, false, err
	}
	if !errors.Is(err, ErrDataDirInUse) {
		return api.ImportResult{}, false, err
	}
	result, err = servicectl.Import(ctx, target, dir, request, run.report)
	return result, true, err
}
