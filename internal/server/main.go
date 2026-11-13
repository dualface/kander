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

	"github.com/dualface/kander/internal/version"
)

// DefaultListen binds loopback; serving a LAN is an explicit choice because
// the first version has no authentication.
const DefaultListen = "127.0.0.1:7878"

// Main runs kander-server until SIGINT or SIGTERM, or an explicit offline
// import, maintenance, project listing or component installation command.
func Main(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "install":
			return runComponentInstall(args[1:])
		case "update":
			return runComponentUpdate(args[1:])
		case "import":
			return runImport(args[1:])
		case "maintain":
			return runMaintain(args[1:])
		case "projects":
			return runProjects(args[1:])
		}
	}
	flags := flag.NewFlagSet("kander-server", flag.ContinueOnError)
	data := flags.String("data", "", "data directory (default: $XDG_DATA_HOME/kander-server or ~/.local/share/kander-server)")
	listen := flags.String("listen", DefaultListen, "listen address")
	ttl := flags.Duration("ttl", DefaultTTL, "claim lease TTL")
	renew := flags.Duration("renew", DefaultRenewInterval, "claim renewal interval announced to clients")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected server arguments")
		return 2
	}
	if *showVersion {
		fmt.Println(version.String())
		return 0
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	dir, err := dataDir(*data)
	if err != nil {
		logger.Error("data directory", "error", err)
		return 1
	}
	s, err := Open(Options{DataDir: dir, TTL: *ttl, RenewInterval: *renew, Logger: logger, Version: version.String()})
	if err != nil {
		logger.Error("open failed", "error", err)
		return 1
	}
	defer s.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		logger.Error("listen failed", "error", err)
		return 1
	}
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	logger.Info("listening", "address", listener.Addr().String(), "version", version.String())
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve failed", "error", err)
		return 1
	}
	return 0
}

func dataDir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "kander-server"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "kander-server"), nil
}

// runImport is `kander-server import`: an explicit, offline migration of one
// v1 board into an empty project. It takes the data directory lock, so it
// refuses to run beside a serving instance.
func runImport(args []string) int {
	flags := flag.NewFlagSet("kander-server import", flag.ContinueOnError)
	data := flags.String("data", "", "data directory")
	name := flags.String("project", "", "project name (the clients' project.name)")
	remoteURL := flags.String("remote", "", "Git remote URL the project binds to")
	from := flags.String("from", "", "v1 board directory (the kanban/ of the main worktree)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *name == "" || *from == "" {
		fmt.Fprintln(os.Stderr, "usage: kander-server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR")
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
