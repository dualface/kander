package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/servicectl"
	"github.com/dualface/kander/internal/version"
)

func init() { cli.Commands["server"] = runServer }

var runForegroundServer = server.RunForeground

func runServer(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, config.CommandUsage("server"))
		return 2
	}
	switch args[0] {
	case "import", "maintain", "projects":
		return server.RunOffline(args)
	}
	cfg, err := config.Load(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	target := config.ServerURL(cfg)
	flags := flag.NewFlagSet("kander server "+args[0], flag.ContinueOnError)
	var o servicectl.StartOptions
	var foreground, jsonOut bool
	switch args[0] {
	case "start":
		flags.StringVar(&o.DataDir, "data", "", config.Text("server.flag_data"))
		flags.StringVar(&o.Listen, "listen", servicectl.ListenAddress(target), config.Text("server.flag_listen"))
		flags.DurationVar(&o.TTL, "ttl", server.DefaultTTL, config.Text("server.flag_ttl"))
		flags.DurationVar(&o.Renew, "renew", server.DefaultRenewInterval, config.Text("server.flag_renew"))
		flags.BoolVar(&foreground, "foreground", false, config.Text("server.flag_foreground"))
	case "status":
		flags.BoolVar(&jsonOut, "json", false, config.Text("server.flag_json"))
	case "stop", "restart":
	default:
		fmt.Fprintln(os.Stderr, config.CommandUsage("server"))
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, config.CommandUsage("server"))
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctl := servicectl.Controller{}
	var info api.ServerInfo
	switch args[0] {
	case "start":
		if o.TTL <= 0 || o.Renew <= 0 || o.Renew >= o.TTL {
			fmt.Fprintln(os.Stderr, config.Text("server.invalid_lease"))
			return 2
		}
		if foreground {
			dir, e := servicectl.DataDir(o.DataDir)
			if e != nil {
				err = e
				break
			}
			err = runForegroundServer(context.Background(), server.Options{DataDir: dir, TTL: o.TTL, RenewInterval: o.Renew, Version: version.String(), Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil))}, o.Listen)
			if err == nil {
				return 0
			}
		} else {
			info, err = ctl.Start(ctx, o)
		}
	case "status":
		info, err = servicectl.Info(ctx, target)
		if client.IsUnreachable(err) {
			fmt.Fprintln(os.Stderr, config.Text("server.not_running", target))
			return 1
		}
	case "stop":
		var running bool
		running, err = ctl.Stop(ctx, target)
		if err == nil {
			if running {
				fmt.Println(config.Text("server.stopped"))
			} else {
				fmt.Println(config.Text("server.not_running", target))
			}
			return 0
		}
	case "restart":
		info, err = ctl.Restart(ctx, target)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, config.Text("server.command_failed", err))
		return 1
	}
	if jsonOut {
		if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		fmt.Println(config.Text("server.info", info.URL, info.Version, info.PID, info.DataDir, info.LogPath, info.Projects, info.Listen, info.TTL.Std(), info.Renew.Std()))
	}
	return 0
}
