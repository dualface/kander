package launch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
)

// hostStartDefaultTimeout is how long kander start --host waits for the order
// to finish; a start on the target host can take two minutes and more.
const hostStartDefaultTimeout = 300

// hostStartPoll bounds one wait for an event of the project between two reads
// of the order.
const hostStartPoll = 20 * time.Second

// takeStartOrder returns the start order the resident worker passed to this
// kander start and removes it from the environment of this process, so no
// agent, terminal server, or later command started from here inherits it.
func takeStartOrder() string {
	order := os.Getenv(process.EnvStartOrder)
	_ = os.Unsetenv(process.EnvStartOrder)
	if order != "" && !api.ValidIdentity(order) {
		return ""
	}
	return order
}

// errHostStartUnsupported reports a server without start orders.
var errHostStartUnsupported = errors.New("start orders are not supported by the server")

// runHostStart is kander start --host: it starts the task on this host when the
// name is this host's, and otherwise asks the server for a start order and
// waits until it is final or the timeout passed.
func runHostStart(host, agent, launcher string, timeout float64, task string) int {
	home, err := remote.Home()
	if err != nil {
		return fail(err)
	}
	self, err := remote.HostName(home)
	if err != nil {
		return fail(err)
	}
	if host == self {
		return runStart(agent, launcher, []string{task})
	}
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCall)
	session, err := remote.Open(ctx, cfg, cwd)
	cancel()
	if err != nil {
		return fail(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), remoteCall)
	order, err := session.Project.CreateStartOrder(ctx, client.NewKey(), api.NewStartOrder{TaskID: task, Host: host, Agent: agent, Launcher: launcher})
	cancel()
	if err != nil {
		return fail(hostStartError(err))
	}
	if !command.JSON() {
		fmt.Fprintln(os.Stderr, t("host.order_created", order.ID, order.TargetHost, order.TaskID))
	}
	order, err = waitStartOrder(session.Project, order, time.Duration(timeout*float64(time.Second)))
	if err != nil {
		return fail(err)
	}
	return startOrderExit(order)
}

// hostStartError explains a server that does not know the route.
func hostStartError(err error) error {
	var e *api.Error
	if errors.As(err, &e) && e.Status == http.StatusNotFound && e.Code == api.CodeInternal {
		return command.Coded("start_order_unsupported", errors.New(t("host.server_unsupported")))
	}
	return err
}

// waitStartOrder reads the order until it is final or timeout passed, waking
// on the events of the project between two reads.
func waitStartOrder(project remote.ProjectService, order api.StartOrder, timeout time.Duration) (api.StartOrder, error) {
	deadline := time.Now().Add(timeout)
	cursor := ""
	for !api.StartOrderFinal(order.State) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return order, nil
		}
		wait := min(remaining, hostStartPoll)
		ctx, cancel := context.WithTimeout(context.Background(), wait+remoteCall)
		page, err := project.Events(ctx, cursor, wait)
		cancel()
		if err == nil {
			cursor = page.Cursor
		} else if !client.IsUnreachable(err) {
			return order, err
		} else {
			time.Sleep(min(time.Second, max(time.Until(deadline), 0)))
		}
		ctx, cancel = context.WithTimeout(context.Background(), remoteCall)
		current, err := project.StartOrder(ctx, order.ID)
		cancel()
		if err == nil {
			order = current
		} else if !client.IsUnreachable(err) {
			return order, err
		}
	}
	return order, nil
}

// startOrderExit reports a final order, or the open one a timeout left.
// started exits 0, another final state 1, and an order that is not final 3.
func startOrderExit(order api.StartOrder) int {
	status := 0
	switch {
	case !api.StartOrderFinal(order.State):
		status = 3
	case order.State != api.StartOrderStarted:
		status = 1
	}
	if command.JSON() {
		return command.ResultStatus(order, status)
	}
	switch status {
	case 0:
		fmt.Fprintln(os.Stdout, t("host.order_started", order.TaskID, order.TargetHost, order.Generation, order.Window))
	case 1:
		code, message := "", ""
		if order.Reason != nil {
			code, message = order.Reason.Code, order.Reason.Message
		}
		fmt.Fprintln(os.Stderr, t("host.order_finished", order.ID, order.State, code, message))
	default:
		fmt.Fprintln(os.Stderr, t("host.order_timeout", order.ID, order.State))
	}
	return status
}
