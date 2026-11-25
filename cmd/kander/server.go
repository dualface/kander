package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
)

func init() { cli.Register(newServerCommand) }

var runForegroundServer = server.RunForeground

// newServerCommand builds kander server: the service lifecycle commands, which this package owns,
// and the offline commands that hold the data directory lock, which internal/server owns.
func newServerCommand() *cobra.Command {
	var start servicectl.StartOptions
	var foreground bool
	startCommand := command.Streaming(&cobra.Command{
		Use:  "start",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runStart(start, foreground)) },
	})
	startFlags := startCommand.Flags()
	startFlags.StringVar(&start.DataDir, "data", "", "")
	startFlags.StringVar(&start.Listen, "listen", "", "")
	startFlags.DurationVar(&start.TTL, "ttl", server.DefaultTTL, "")
	startFlags.DurationVar(&start.Renew, "renew", server.DefaultRenewInterval, "")
	startFlags.BoolVar(&foreground, "foreground", false, "")
	status := &cobra.Command{
		Use:  "status",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runStatus()) },
	}
	return command.Group("server",
		startCommand,
		&cobra.Command{Use: "stop", Args: command.Args(0, 0), RunE: func(*cobra.Command, []string) error { return command.Exit(runStop()) }},
		&cobra.Command{Use: "restart", Args: command.Args(0, 0), RunE: func(*cobra.Command, []string) error { return command.Exit(runRestart()) }},
		status,
		server.NewImportCommand(),
		server.NewMaintainCommand(),
		server.NewProjectsCommand(),
	)
}

// serverTarget loads the effective configuration and returns the server URL it names.
func serverTarget() (string, error) {
	_, target, err := serverTargetConfig()
	return target, err
}

// serverTargetConfig is serverTarget that also returns the configuration it loaded.
func serverTargetConfig() (*config.Config, string, error) {
	cfg, err := config.Load(true)
	if err != nil {
		return nil, "", err
	}
	return cfg, config.ServerURL(cfg), nil
}

// failServer reports a failure of a server command with its localized wrapper text.
func failServer(err error) int {
	if command.JSON() {
		code, _ := command.Describe(err)
		return command.Fail(&command.Failure{Code: code, Message: config.Text("server.command_failed", err), Err: err})
	}
	fmt.Fprintln(os.Stderr, config.Text("server.command_failed", err))
	return 1
}

// failText reports a failure that has a text of its own and a stable code.
func failText(code, text string) int {
	if command.JSON() {
		return command.Fail(command.Coded(code, errors.New(text)))
	}
	fmt.Fprintln(os.Stderr, text)
	return 1
}

func printInfo(info api.ServerInfo) int {
	if command.JSON() {
		return command.Result(info)
	}
	fmt.Println(config.Text("server.info", info.URL, info.Version, info.PID, info.DataDir, info.LogPath, info.Projects, info.Listen, info.TTL.Std(), info.Renew.Std()))
	return 0
}

// foregroundReady is the object kander server start --foreground --json writes once the server
// can serve.
type foregroundReady struct {
	Listen  string `json:"listen"`
	DataDir string `json:"data_dir"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

// serverLog is the log writer of the foreground server. Before the server can serve, with --json,
// every line is a remark of the command (the notice server_log), so that a failure before the
// first line of stdout is one error object that holds them; afterwards, and without --json, the
// lines go to stderr as they are.
type serverLog struct{ served atomic.Bool }

func (l *serverLog) ready() { l.served.Store(true) }

func (l *serverLog) Write(p []byte) (int, error) {
	if l.served.Load() {
		return os.Stderr.Write(p)
	}
	command.Notify("server_log", strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

func runStart(o servicectl.StartOptions, foreground bool) int {
	cfg, target, err := serverTargetConfig()
	if err != nil {
		return command.Fail(err)
	}
	if o.Listen == "" {
		o.Listen = servicectl.ListenAddress(target)
	}
	if o.TTL <= 0 || o.Renew <= 0 || o.Renew >= o.TTL {
		return command.FailStatus(2, command.Coded(command.CodeUsage, errors.New(config.Text("server.invalid_lease"))))
	}
	if foreground {
		dir, err := servicectl.DataDir(o.DataDir)
		if err == nil {
			logs := &serverLog{}
			options := server.Options{DataDir: dir, TTL: o.TTL, RenewInterval: o.Renew, Version: version.String(), Logger: slog.New(slog.NewJSONHandler(logs, nil))}
			// The background start runs this path in its child, so the warnings are written
			// once, by the process that serves. With --json they are server_log notices.
			config.LogIgnored(options.Logger, cfg)
			if command.JSON() {
				options.Ready = func(listen string) {
					command.Emit(foregroundReady{Listen: listen, DataDir: dir, PID: os.Getpid(), Version: version.String()})
					logs.ready()
				}
			} else {
				logs.ready()
			}
			err = runForegroundServer(context.Background(), options, o.Listen)
		}
		if err != nil {
			return failServer(err)
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := servicectl.Controller{}.Start(ctx, o)
	if err != nil {
		return failServer(err)
	}
	return printInfo(info)
}

func runStatus() int {
	target, err := serverTarget()
	if err != nil {
		return command.Fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := servicectl.Info(ctx, target)
	if client.IsUnreachable(err) {
		return failText("server_not_running", config.Text("server.not_running", target))
	}
	if err != nil {
		return failServer(err)
	}
	if command.JSON() {
		return command.Result(info)
	}
	return printInfo(info)
}

func runStop() int {
	target, err := serverTarget()
	if err != nil {
		return command.Fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	running, err := servicectl.Controller{}.Stop(ctx, target)
	if err != nil {
		return failServer(err)
	}
	if command.JSON() {
		return command.Result(struct {
			Stopped bool   `json:"stopped"`
			Target  string `json:"target"`
		}{running, target})
	}
	if running {
		fmt.Println(config.Text("server.stopped"))
	} else {
		fmt.Println(config.Text("server.not_running", target))
	}
	return 0
}

func runRestart() int {
	target, err := serverTarget()
	if err != nil {
		return command.Fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := servicectl.Controller{}.Restart(ctx, target)
	if err != nil {
		return failServer(err)
	}
	return printInfo(info)
}
