package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
	"golang.org/x/term"
)

// DefaultListen binds loopback; serving a LAN is an explicit choice because
// the first version has no authentication.
const DefaultListen = api.DefaultListen

// RunOffline dispatches commands that hold the data lock without serving HTTP.
func RunOffline(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "import":
			return runImport(args[1:])
		case "maintain":
			return runMaintain(args[1:])
		case "projects":
			return runProjects(args[1:])
		}
	}
	return 2
}

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
	err = httpServer.Serve(listener)
	stop()
	<-finished
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func dataDir(value string) (string, error) { return servicectl.DataDir(value) }

// runImport is `kander server import`. When no process holds the data
// directory lock it imports offline; when a serving instance holds it, that
// instance imports through its administration route. Without --from the
// source is the board v1 commands locate. The progress goes to stderr and the
// result to stdout.
func runImport(args []string) int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	return importCommand(args, os.Stdout, os.Stderr, term.IsTerminal(int(os.Stderr.Fd())), signals)
}

// importCommand is runImport with its streams and interrupt source given. A
// receive on signals is one Ctrl+C.
func importCommand(args []string, stdout, stderr io.Writer, tty bool, signals chan os.Signal) int {
	flags := flag.NewFlagSet("kander server import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	data := flags.String("data", "", "data directory")
	name := flags.String("project", "", "project name (the clients' project.name)")
	remoteURL := flags.String("remote", "", "Git remote URL the project binds to")
	from := flags.String("from", "", "v1 board directory (default: the kanban/ that v1 commands locate)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *name == "" {
		fmt.Fprintln(stderr, "usage: kander server import [--data DIR] --project NAME [--remote URL] [--from KANBAN_DIR]")
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	dir, err := dataDir(*data)
	if err != nil {
		logger.Error("data directory", "error", err)
		return 1
	}
	source, err := importSource(*from)
	if err != nil {
		logger.Error("source board", "error", err)
		return 1
	}
	report := newImportReporter(stderr, tty)
	interrupt := watchImportInterrupt(report, signals)
	defer interrupt.stop()
	result, online, err := importBoard(interrupt.ctx, dir, *name, *remoteURL, source, importRun{logger: logger, report: report.Report, offline: &interrupt.offline})
	report.Finish()
	if err != nil {
		if interrupt.cancelled() {
			if online && report.PastCopy() {
				report.Notice(config.Text("server.import_cancelled_late"))
			} else {
				report.Notice(config.Text("server.import_cancelled"))
			}
			return 130
		}
		logger.Error("import failed", "error", err)
		return 1
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
	ctx     context.Context
	signals chan os.Signal
	cancel  context.CancelFunc
	offline atomic.Bool
	hit     atomic.Bool
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
	logger  *slog.Logger
	report  func(api.ImportProgress)
	offline *atomic.Bool
}

// importOpen opens the data directory for an offline import; tests replace it.
var importOpen = Open

// importBoard imports offline under the data directory lock, or through the
// running server that holds it, and says which one it used.
func importBoard(ctx context.Context, dir, name, remote, source string, run importRun) (result api.ImportResult, online bool, err error) {
	s, err := importOpen(Options{DataDir: dir, Logger: run.logger, Version: version.String()})
	if err == nil {
		defer s.Close()
		if run.offline != nil {
			run.offline.Store(true)
		}
		ctx, cancel := context.WithTimeout(ctx, ImportTimeout)
		defer cancel()
		result, err = s.ImportWithProgress(ctx, name, remote, source, run.report)
		return result, false, err
	}
	if !errors.Is(err, ErrDataDirInUse) {
		return api.ImportResult{}, false, err
	}
	target, err := importTarget()
	if err != nil {
		return api.ImportResult{}, true, err
	}
	result, err = servicectl.Import(ctx, target, dir, api.ImportRequest{Project: name, Remote: remote, From: source}, run.report)
	return result, true, err
}
