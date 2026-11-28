package servicecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// operationTimeout bounds one kander service command, the stop of a detached worker included.
const operationTimeout = 2 * time.Minute

// statusArgs accepts at most one service name.
func statusArgs(c *cobra.Command, args []string) error {
	if err := command.Args(0, 1)(c, args); err != nil {
		return err
	}
	for _, arg := range args {
		if _, ok := svcinstall.ParseKind(arg); !ok {
			return command.Usage("service.unknown_service", command.Escape(arg))
		}
	}
	return nil
}

// NewCommand builds kander service. install and uninstall take the service as a subcommand, so
// the server options are flags of install server only.
func NewCommand() *cobra.Command {
	var server ServerOptions
	installServer := &cobra.Command{
		Use:  "server",
		Args: command.Args(0, 0),
		RunE: func(c *cobra.Command, _ []string) error {
			return command.Exit(runInstall(svcinstall.KindServer, server, c))
		},
	}
	flags := installServer.Flags()
	flags.StringVar(&server.Listen, "listen", "", "")
	flags.StringVar(&server.DataDir, "data", "", "")
	flags.DurationVar(&server.TTL, "ttl", api.DefaultTTL, "")
	flags.DurationVar(&server.Renew, "renew", api.DefaultRenewInterval, "")
	installWorker := &cobra.Command{
		Use:  "worker",
		Args: command.Args(0, 0),
		RunE: func(c *cobra.Command, _ []string) error {
			return command.Exit(runInstall(svcinstall.KindWorker, ServerOptions{}, c))
		},
	}
	uninstall := func(kind svcinstall.Kind) *cobra.Command {
		return &cobra.Command{Use: string(kind), Args: command.Args(0, 0), RunE: func(*cobra.Command, []string) error {
			return command.Exit(runUninstall(kind))
		}}
	}
	return command.Group("service",
		command.Group("install", installServer, installWorker),
		command.Group("uninstall", uninstall(svcinstall.KindServer), uninstall(svcinstall.KindWorker)),
		&cobra.Command{Use: "status", Args: statusArgs, RunE: func(_ *cobra.Command, args []string) error {
			kinds := svcinstall.Kinds
			if len(args) == 1 {
				kind, _ := svcinstall.ParseKind(args[0])
				kinds = []svcinstall.Kind{kind}
			}
			return command.Exit(runStatus(kinds))
		}},
	)
}

// fail reports a failure; a definition Kander does not own has its own code.
func fail(err error) int {
	var foreign *svcinstall.ForeignError
	if errors.As(err, &foreign) {
		return command.Fail(command.Coded("service_foreign_definition", err))
	}
	return command.Fail(err)
}

func runInstall(kind svcinstall.Kind, server ServerOptions, c *cobra.Command) int {
	if kind == svcinstall.KindServer {
		defaults, err := defaultServerOptions()
		if err != nil {
			return command.Fail(err)
		}
		if !c.Flags().Changed("listen") {
			server.Listen = defaults.Listen
		}
		if server.TTL <= 0 || server.Renew <= 0 || server.Renew >= server.TTL {
			return command.FailStatus(2, command.Coded(command.CodeUsage, errors.New(config.Text("server.invalid_lease"))))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	result, err := Install(ctx, kind, server)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Result(result)
	}
	if result.Stopped != "" {
		fmt.Println(config.Text("service.stopped_detached", result.Stopped))
	}
	switch {
	case !result.Changed:
		fmt.Println(config.Text("service.installed_unchanged", kind, result.Location))
	case result.Restarted:
		fmt.Println(config.Text("service.installed_restarted", kind, result.Location))
	default:
		fmt.Println(config.Text("service.installed", kind, result.Location))
	}
	fmt.Println(config.Text("service.status_command", strings.Join(append([]string{result.Executable}, result.Args...), " ")))
	if result.Linger == "no" {
		fmt.Fprintln(os.Stderr, config.Text("service.linger_hint", svcinstall.LingerUser()))
	}
	return 0
}

func runUninstall(kind svcinstall.Kind) int {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	location, found, err := Uninstall(ctx, kind)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Result(struct {
			Service  string `json:"service"`
			Location string `json:"location"`
			Removed  bool   `json:"removed"`
		}{string(kind), location, found})
	}
	if found {
		fmt.Println(config.Text("service.uninstalled", kind, location))
	} else {
		fmt.Println(config.Text("service.not_installed", kind, location))
	}
	return 0
}

func runStatus(kinds []svcinstall.Kind) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	statuses := Inspect(ctx, kinds)
	if command.JSON() {
		return command.Result(struct {
			Services []Status `json:"services"`
		}{statuses})
	}
	for _, st := range statuses {
		headline, details := st.Lines()
		fmt.Println(headline)
		for _, line := range details {
			fmt.Println("  " + line)
		}
		for _, warning := range st.Warnings {
			fmt.Println("  " + config.Text("service.status_warning", warning))
		}
	}
	return 0
}
