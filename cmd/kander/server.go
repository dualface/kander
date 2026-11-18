package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
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
	var foreground, jsonOut bool
	startCommand := &cobra.Command{
		Use:  "start",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runStart(start, foreground)) },
	}
	startFlags := startCommand.Flags()
	startFlags.StringVar(&start.DataDir, "data", "", "")
	startFlags.StringVar(&start.Listen, "listen", "", "")
	startFlags.DurationVar(&start.TTL, "ttl", server.DefaultTTL, "")
	startFlags.DurationVar(&start.Renew, "renew", server.DefaultRenewInterval, "")
	startFlags.BoolVar(&foreground, "foreground", false, "")
	status := &cobra.Command{
		Use:  "status",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runStatus(jsonOut)) },
	}
	status.Flags().BoolVar(&jsonOut, "json", false, "")
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
	cfg, err := config.Load(true)
	if err != nil {
		return "", err
	}
	return config.ServerURL(cfg), nil
}

func failServer(err error) int {
	fmt.Fprintln(os.Stderr, config.Text("server.command_failed", err))
	return 1
}

func printInfo(info api.ServerInfo) int {
	fmt.Println(config.Text("server.info", info.URL, info.Version, info.PID, info.DataDir, info.LogPath, info.Projects, info.Listen, info.TTL.Std(), info.Renew.Std()))
	return 0
}

func runStart(o servicectl.StartOptions, foreground bool) int {
	target, err := serverTarget()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if o.Listen == "" {
		o.Listen = servicectl.ListenAddress(target)
	}
	if o.TTL <= 0 || o.Renew <= 0 || o.Renew >= o.TTL {
		fmt.Fprintln(os.Stderr, config.Text("server.invalid_lease"))
		return 2
	}
	if foreground {
		dir, err := servicectl.DataDir(o.DataDir)
		if err == nil {
			err = runForegroundServer(context.Background(), server.Options{DataDir: dir, TTL: o.TTL, RenewInterval: o.Renew, Version: version.String(), Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil))}, o.Listen)
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

func runStatus(jsonOut bool) int {
	target, err := serverTarget()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := servicectl.Info(ctx, target)
	if client.IsUnreachable(err) {
		fmt.Fprintln(os.Stderr, config.Text("server.not_running", target))
		return 1
	}
	if err != nil {
		return failServer(err)
	}
	if jsonOut {
		if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	return printInfo(info)
}

func runStop() int {
	target, err := serverTarget()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	running, err := servicectl.Controller{}.Stop(ctx, target)
	if err != nil {
		return failServer(err)
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
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := servicectl.Controller{}.Restart(ctx, target)
	if err != nil {
		return failServer(err)
	}
	return printInfo(info)
}
