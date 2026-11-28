package remote

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
)

// hostCommandTimeout bounds one call of kander host and kander order.
const hostCommandTimeout = 30 * time.Second

// NewHostCommand builds kander host, which lists the hosts of the configured server.
func NewHostCommand() *cobra.Command {
	list := &cobra.Command{
		Use:  "list",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return runHostList() },
	}
	return command.Group("host", list)
}

// NewOrderCommand builds kander order, which reads and withdraws start orders.
func NewOrderCommand() *cobra.Command {
	show := &cobra.Command{
		Use:  "show <order-id>",
		Args: orderArgs,
		RunE: func(_ *cobra.Command, args []string) error { return runOrder(args[0], false) },
	}
	cancel := &cobra.Command{
		Use:  "cancel <order-id>",
		Args: orderArgs,
		RunE: func(_ *cobra.Command, args []string) error { return runOrder(args[0], true) },
	}
	return command.Group("order", command.RequireProject(show), command.RequireProject(cancel))
}

func orderArgs(c *cobra.Command, args []string) error {
	if err := command.Args(1, 1)(c, args); err != nil {
		return err
	}
	if !api.ValidIdentity(args[0]) {
		return command.Usage("host.invalid_order_id", command.Escape(args[0]))
	}
	return nil
}

func runHostList() error {
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	home, err := Home()
	if err != nil {
		return fail(err)
	}
	id, err := ClientID(home)
	if err != nil {
		return fail(err)
	}
	c, err := client.New(config.ServerURL(cfg), id)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCommandTimeout)
	defer cancel()
	list, err := c.Hosts(ctx)
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(list))
	}
	if len(list.Hosts) == 0 {
		fmt.Fprintln(stdout, config.Text("host.list_empty"))
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, config.Text("host.list_header"))
	for _, h := range list.Hosts {
		state := config.Text("host.offline")
		if h.Online {
			state = config.Text("host.online")
		}
		accepting := config.Text("host.accepting_no")
		if h.Accepting {
			accepting = config.Text("host.accepting_yes")
		}
		conflict := ""
		if h.Conflict != "" {
			conflict = " " + config.Text("host.list_conflict", h.Conflict)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s%s\n", h.Name, state, h.LastHeartbeat.Local().Format("2006-01-02 15:04:05"), h.Version, accepting, conflict)
		for _, p := range h.Projects {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", p.Project, hostNames(p.Agents), hostNames(p.Launchers))
		}
	}
	return w.Flush()
}

func hostNames(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ",")
}

func runOrder(id string, cancelOrder bool) error {
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCommandTimeout)
	defer cancel()
	session, err := Open(ctx, cfg, cwd)
	if err != nil {
		return fail(err)
	}
	var order api.StartOrder
	if cancelOrder {
		order, err = session.Project.CancelStartOrder(ctx, client.NewKey(), id)
	} else {
		order, err = session.Project.StartOrder(ctx, id)
	}
	if err != nil {
		return fail(err)
	}
	if command.JSON() {
		return command.Exit(command.Result(order))
	}
	PrintStartOrder(stdout, order)
	return nil
}

// PrintStartOrder writes the text form of a start order.
func PrintStartOrder(w interface{ Write([]byte) (int, error) }, o api.StartOrder) {
	fmt.Fprintln(w, config.Text("host.order_line", o.ID, o.State, o.TaskID, o.TargetHost))
	if o.Agent != "" || o.Launcher != "" {
		fmt.Fprintln(w, config.Text("host.order_choice", hostNames(nonEmpty(o.Agent)), hostNames(nonEmpty(o.Launcher))))
	}
	if o.Generation != 0 {
		fmt.Fprintln(w, config.Text("host.order_generation", o.Generation, o.Window))
	}
	if o.Reason != nil {
		fmt.Fprintln(w, config.Text("host.order_reason", o.Reason.Code, o.Reason.Message))
	}
	fmt.Fprintln(w, config.Text("host.order_times", o.CreatedAt.Local().Format("2006-01-02 15:04:05"), o.UpdatedAt.Local().Format("2006-01-02 15:04:05")))
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
