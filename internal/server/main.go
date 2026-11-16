package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
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
// source is the board v1 commands locate.
func runImport(args []string) int {
	flags := flag.NewFlagSet("kander server import", flag.ContinueOnError)
	data := flags.String("data", "", "data directory")
	name := flags.String("project", "", "project name (the clients' project.name)")
	remoteURL := flags.String("remote", "", "Git remote URL the project binds to")
	from := flags.String("from", "", "v1 board directory (default: the kanban/ that v1 commands locate)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *name == "" {
		fmt.Fprintln(os.Stderr, "usage: kander server import [--data DIR] --project NAME [--remote URL] [--from KANBAN_DIR]")
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
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
	result, err := importBoard(dir, *name, *remoteURL, source, logger)
	if err != nil {
		logger.Error("import failed", "error", err)
		return 1
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	return 0
}

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

// importBoard imports offline under the data directory lock, or through the
// running server that holds it.
func importBoard(dir, name, remote, source string, logger *slog.Logger) (api.ImportResult, error) {
	s, err := Open(Options{DataDir: dir, Logger: logger, Version: version.String()})
	if err == nil {
		defer s.Close()
		return s.Import(name, remote, source)
	}
	if !errors.Is(err, ErrDataDirInUse) {
		return api.ImportResult{}, err
	}
	target, err := importTarget()
	if err != nil {
		return api.ImportResult{}, err
	}
	return servicectl.Import(context.Background(), target, dir, api.ImportRequest{Project: name, Remote: remote, From: source})
}
