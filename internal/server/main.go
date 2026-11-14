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
	"syscall"
	"time"

	"github.com/dualface/kander/internal/api"
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

// runImport is `kander server import`: an explicit, offline migration of one
// v1 board into an empty project. It takes the data directory lock, so it
// refuses to run beside a serving instance.
func runImport(args []string) int {
	flags := flag.NewFlagSet("kander server import", flag.ContinueOnError)
	data := flags.String("data", "", "data directory")
	name := flags.String("project", "", "project name (the clients' project.name)")
	remoteURL := flags.String("remote", "", "Git remote URL the project binds to")
	from := flags.String("from", "", "v1 board directory (the kanban/ of the main worktree)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *name == "" || *from == "" {
		fmt.Fprintln(os.Stderr, "usage: kander server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR")
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	dir, err := dataDir(*data)
	if err != nil {
		logger.Error("data directory", "error", err)
		return 1
	}
	s, err := Open(Options{DataDir: dir, Logger: logger, Version: version.String()})
	if err != nil {
		logger.Error("open failed", "error", err)
		return 1
	}
	defer s.Close()
	result, err := s.Import(*name, *remoteURL, *from)
	if err != nil {
		logger.Error("import failed", "error", err)
		return 1
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	return 0
}
