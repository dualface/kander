package worker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
)

// The resident worker of a host takes the start orders other hosts direct at
// it. For every server of the projects registered in host.json it sends a
// heartbeat that long-polls for orders, accepts each pending order at once, and
// runs `kander start` for the accepted orders one after the other, as a child
// in the registered directory of the project. The child is detached, so a
// worker that exits or restarts leaves it and the agent it starts running.

// Defaults of the host agent; Options overrides them in tests.
const (
	defaultHostRescan     = 5 * time.Second
	defaultHeartbeatWait  = api.HostHeartbeatMaxWait
	maxHeartbeatBackoff   = 60 * time.Second
	minReportBackoff      = 2 * time.Second
	maxReportBackoff      = 60 * time.Second
	startChildTimeout     = 15 * time.Minute
	startChildOutputLimit = 1 << 20
)

// hostAgent runs the heartbeats of one worker and its queue of orders.
type hostAgent struct {
	o        Options
	workerID string
	log      *slog.Logger
	queue    chan queuedOrder
	// mu guards known, outcomes, and loops. known holds the orders this worker
	// queued, with their server, so an order is run once per worker process; a
	// new process runs the orders accepted earlier that are not final again, and
	// the launch receipt of the order keeps that from starting a second agent.
	// An order a heartbeat of its server no longer lists is final and forgotten.
	mu    sync.Mutex
	known map[string]string
	// outcomes holds the failure of each order this worker has not reported
	// yet: the read or the report did not reach the server. A heartbeat that
	// still lists the order queues the report again once its backoff passed,
	// with the same idempotency key, until the server records it or a read shows
	// the order ended or bound to a claim.
	outcomes map[string]*orderOutcome
	loops    map[string]context.CancelFunc
	wg       sync.WaitGroup
}

// orderOutcome is a failure of an order that waits for its report.
type orderOutcome struct {
	failure api.StartOrderFailure
	key     string
	due     time.Time
	backoff time.Duration
	queued  bool
}

func newWorkerID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "w-" + hex.EncodeToString(b)
}

// startHostAgent starts the host agent when the worker takes orders and
// returns the function that stops it.
func (w *loop) startHostAgent(ctx context.Context) func() {
	if !w.o.AcceptOrders {
		return func() {}
	}
	h := &hostAgent{o: w.o, workerID: newWorkerID(), log: w.o.Logger.With("component", "host"), queue: make(chan queuedOrder, 64), known: map[string]string{}, outcomes: map[string]*orderOutcome{}, loops: map[string]context.CancelFunc{}}
	ctx, cancel := context.WithCancel(ctx)
	h.wg.Add(2)
	go h.supervise(ctx)
	go h.execute(ctx)
	h.log.Info("host agent started", "worker_id", h.workerID)
	return func() {
		cancel()
		h.wg.Wait()
	}
}

// supervise keeps one heartbeat loop per server of the registered projects,
// reading host.json again every rescan interval.
func (h *hostAgent) supervise(ctx context.Context) {
	defer h.wg.Done()
	ticker := time.NewTicker(h.o.HostRescan)
	defer ticker.Stop()
	for {
		record, err := remote.ReadHostRecord(h.o.Home)
		if err != nil {
			h.log.Warn("host.json could not be read", "error", err)
		} else {
			h.reconcileLoops(ctx, record.ServerURLs())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *hostAgent) reconcileLoops(ctx context.Context, urls []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for url, stop := range h.loops {
		if !slices.Contains(urls, url) {
			stop()
			delete(h.loops, url)
		}
	}
	for _, url := range urls {
		if _, running := h.loops[url]; running {
			continue
		}
		loopCtx, stop := context.WithCancel(ctx)
		h.loops[url] = stop
		h.wg.Add(1)
		go h.heartbeats(loopCtx, url)
	}
}

// heartbeats reports this host to one server until ctx ends. A heartbeat
// returns at once when an order is pending for this host, else after the
// long-poll wait, and the next one follows at once; failures back off.
func (h *hostAgent) heartbeats(ctx context.Context, url string) {
	defer h.wg.Done()
	log := h.log.With("server", url)
	backoff := time.Duration(0)
	for ctx.Err() == nil {
		if backoff > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
		result, c, err := h.beat(ctx, url)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			backoff = min(max(2*backoff, 5*time.Second), maxHeartbeatBackoff)
			if api.IsCode(err, api.CodeHostNameConflict) {
				log.Error("the server refused the host name: another client uses it; set host.name in config.json", "error", err)
			} else {
				log.Warn("host heartbeat failed", "error", err, "retry_in", backoff)
			}
			continue
		}
		backoff = 0
		if result.Host.Conflict != "" {
			log.Error("the server saw two workers of this client ID", "conflict", result.Host.Conflict)
		}
		if h.takeOrders(ctx, log, c, result.Orders) {
			// An order stays pending after a failed acceptance; the server then
			// answers the next heartbeat at once, so it waits a moment first.
			backoff = time.Second
		}
	}
}

// beat sends one heartbeat with the projects of the server that pass the
// registration check.
func (h *hostAgent) beat(ctx context.Context, url string) (api.HostHeartbeatResult, *client.Client, error) {
	id, err := remote.ClientID(h.o.Home)
	if err != nil {
		return api.HostHeartbeatResult{}, nil, err
	}
	name, err := remote.HostName(h.o.Home)
	if err != nil {
		return api.HostHeartbeatResult{}, nil, err
	}
	c, err := client.New(url, id)
	if err != nil {
		return api.HostHeartbeatResult{}, nil, err
	}
	c.Retries = 0
	record, err := remote.ReadHostRecord(h.o.Home)
	if err != nil {
		return api.HostHeartbeatResult{}, nil, err
	}
	hb := api.HostHeartbeat{ClientID: id, Name: name, WorkerID: h.workerID, Version: h.o.Version(), Accepting: true, OrderProtocol: api.StartOrderProtocol, Projects: []api.HostProject{}}
	for _, reg := range record.Projects {
		if reg.ServerURL != url {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := checkRegistration(checkCtx, c, reg)
		cancel()
		if err != nil {
			h.log.Debug("registered project is not reported", "project", reg.Project, "root", reg.Root, "error", err)
			continue
		}
		project := api.HostProject{Project: reg.Project, Agents: []string{}, Launchers: []string{}}
		if scan, ok := record.ScanOf(url, reg.Project); ok {
			for _, agent := range scan.Agents {
				project.Agents = append(project.Agents, agent.Name)
			}
			project.Launchers = append(project.Launchers, scan.Launchers...)
		}
		hb.Projects = append(hb.Projects, project)
	}
	callCtx, cancel := context.WithTimeout(ctx, h.o.HeartbeatWait+30*time.Second)
	defer cancel()
	result, err := c.Heartbeat(callCtx, hb, h.o.HeartbeatWait)
	return result, c, err
}

// errRegistration marks a registration check that failed.
type errRegistration struct{ reason string }

func (e errRegistration) Error() string { return e.reason }

// checkRegistration verifies that a registered directory still is the
// checkout of the project: it exists, it is a Git checkout, its configuration
// binds the same server and project, and its origin is the Git remote the
// server registered for the project.
func checkRegistration(ctx context.Context, c *client.Client, reg remote.ProjectRegistration) error {
	info, err := os.Stat(reg.Root)
	if err != nil || !info.IsDir() {
		return errRegistration{config.Text("host.check_missing_dir", reg.Root)}
	}
	if _, ok := remote.RegistrationRoot(reg.Root); !ok {
		return errRegistration{config.Text("host.check_not_git", reg.Root)}
	}
	cfg, err := config.LoadFor(reg.Root)
	if err != nil {
		return errRegistration{config.Text("host.check_config", reg.Root, err.Error())}
	}
	if config.ServerURL(cfg) != reg.ServerURL || cfg.Project.Name != reg.Project {
		return errRegistration{config.Text("host.check_binding", reg.Root, reg.Project, reg.ServerURL)}
	}
	project, err := c.ProjectByName(ctx, reg.Project)
	if err != nil {
		return err
	}
	origin, err := remote.ResolveGitRemote(ctx, reg.Root)
	if err != nil {
		return err
	}
	if project.Remote == "" || origin != project.Remote {
		return errRegistration{config.Text("host.check_remote", reg.Root, origin, project.Remote)}
	}
	return nil
}

// takeOrders accepts the pending orders of a heartbeat answer and queues them,
// queues the accepted orders this worker did not run, and queues the failure
// reports of accepted orders that are due again. It reports whether an
// acceptance failed.
func (h *hostAgent) takeOrders(ctx context.Context, log *slog.Logger, c *client.Client, orders []api.StartOrder) bool {
	h.forgetFinished(c.ServerURL(), orders)
	failed := false
	for _, order := range orders {
		if order.TargetClientID != c.ClientID() {
			continue
		}
		if order.State == api.StartOrderPending {
			callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			accepted, err := c.Project(order.ProjectID).AcceptStartOrder(callCtx, client.NewKey(), order.ID)
			cancel()
			if err != nil {
				failed = true
				log.Warn("start order could not be accepted", "order", order.ID, "task", order.TaskID, "error", err)
				continue
			}
			log.Info("start order accepted", "order", order.ID, "task", order.TaskID)
			order = accepted
		}
		if order.State != api.StartOrderAccepted {
			continue
		}
		h.mu.Lock()
		_, known := h.known[order.ID]
		h.known[order.ID] = c.ServerURL()
		report := false
		if out := h.outcomes[order.ID]; known && out != nil && !out.queued && !time.Now().Before(out.due) {
			out.queued, report = true, true
		}
		h.mu.Unlock()
		if known && !report {
			continue
		}
		select {
		case h.queue <- queuedOrder{order: order, client: c, report: report}:
		case <-ctx.Done():
			return failed
		}
	}
	return failed
}

// forgetFinished drops the known orders of a server that its heartbeat answer
// no longer lists: they are final.
func (h *hostAgent) forgetFinished(url string, orders []api.StartOrder) {
	listed := map[string]bool{}
	for _, order := range orders {
		listed[order.ID] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, server := range h.known {
		if server == url && !listed[id] {
			delete(h.known, id)
			delete(h.outcomes, id)
		}
	}
}

// queuedOrder is an accepted order with the client of its server; report
// queues the pending failure report of the order instead of its start.
type queuedOrder struct {
	order  api.StartOrder
	client *client.Client
	report bool
}

// execute runs the queued orders one at a time: one repository and one
// terminal take one start at a time, as a person running kander start would.
func (h *hostAgent) execute(ctx context.Context) {
	defer h.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case queued := <-h.queue:
			if queued.report {
				h.settle(ctx, h.log.With("order", queued.order.ID, "task", queued.order.TaskID), queued.client.Project(queued.order.ProjectID), queued.order.ID)
				continue
			}
			h.run(ctx, queued.client, queued.order)
		}
	}
}

// run starts the task of one accepted order in the registered directory of its
// project, then reports a failure the start left before its claim.
func (h *hostAgent) run(ctx context.Context, c *client.Client, order api.StartOrder) {
	log := h.log.With("order", order.ID, "task", order.TaskID)
	project := c.Project(order.ProjectID)
	// An order cancelled or ended while it waited in the queue is not run.
	readCtx, cancelRead := context.WithTimeout(ctx, 30*time.Second)
	current, err := project.StartOrder(readCtx, order.ID)
	cancelRead()
	if err == nil && current.State != api.StartOrderAccepted {
		log.Info("start order ended before its start ran", "state", current.State)
		return
	}
	record, err := remote.ReadHostRecord(h.o.Home)
	if err != nil {
		h.fail(ctx, log, project, order, "host_record_unreadable", err.Error())
		return
	}
	reg, found := registrationFor(record, c, order.ProjectID)
	if !found {
		h.fail(ctx, log, project, order, "registration_missing", config.Text("host.check_unregistered", order.ProjectID))
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = checkRegistration(checkCtx, c, reg)
	cancel()
	if err != nil {
		h.fail(ctx, log, project, order, "registration_invalid", err.Error())
		return
	}
	exe, err := h.o.StartExecutable()
	if err != nil {
		h.fail(ctx, log, project, order, "executable_unknown", err.Error())
		return
	}
	scan, _ := record.ScanOf(reg.ServerURL, reg.Project)
	cmd := exec.Command(exe, startArguments(order)...)
	cmd.Dir = reg.Root
	cmd.Env = startEnvironment(h.o.Home, order.ID, record, scan)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limited{w: &stdout, n: startChildOutputLimit}, &limited{w: &stderr, n: startChildOutputLimit}
	process.Detach(cmd)
	log.Info("running kander start for the start order", "dir", reg.Root, "agent", order.Agent, "launcher", order.Launcher)
	if err := cmd.Start(); err != nil {
		h.fail(ctx, log, project, order, "start_not_run", err.Error())
		return
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		// The detached start keeps running; the next worker reads the order.
		log.Info("worker stops while kander start runs; the start continues")
		return
	case <-time.After(startChildTimeout):
		_ = cmd.Process.Kill()
		runErr = <-done
	}
	code, message := childFailure(stderr.Bytes(), runErr)
	log.Info("kander start finished", "error", runErr, "code", code)
	if code == "" {
		code, message = "start_without_claim", config.Text("host.start_without_claim")
	}
	// Only a start that never claimed leaves the order to the worker: a claim
	// binds the generation, whose confirmation or end the server records. The
	// report reads the order first and drops the failure of a bound order.
	h.fail(ctx, log, project, order, code, message)
}

// fail records the failure of an order and reports it.
func (h *hostAgent) fail(ctx context.Context, log *slog.Logger, project *client.Project, order api.StartOrder, code, message string) {
	if len(message) > 2000 {
		message = message[:2000]
	}
	h.mu.Lock()
	h.outcomes[order.ID] = &orderOutcome{failure: api.StartOrderFailure{Code: code, Message: message}, key: client.NewKey()}
	h.mu.Unlock()
	h.settle(ctx, log, project, order.ID)
}

// settle reports the pending failure of an order while a read shows the order
// accepted and not bound to a claim; the failure of an order that ended or
// bound a generation is dropped. A read or a report that fails keeps the
// failure, and a later heartbeat queues it again after a growing backoff.
func (h *hostAgent) settle(ctx context.Context, log *slog.Logger, project *client.Project, id string) {
	h.mu.Lock()
	out := h.outcomes[id]
	if out == nil {
		h.mu.Unlock()
		return
	}
	out.queued = false
	failure, key := out.failure, out.key
	h.mu.Unlock()
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	current, err := project.StartOrder(readCtx, id)
	cancel()
	if err == nil {
		if current.State != api.StartOrderAccepted || current.Generation != 0 {
			h.dropOutcome(id)
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = project.FailStartOrder(callCtx, key, id, failure)
		cancel()
		if err == nil {
			h.dropOutcome(id)
			log.Info("start order failed", "code", failure.Code, "message", failure.Message)
			return
		}
	}
	if api.IsCode(err, api.CodeNotFound) {
		h.dropOutcome(id)
		return
	}
	h.mu.Lock()
	if out := h.outcomes[id]; out != nil {
		out.backoff = min(max(2*out.backoff, minReportBackoff), maxReportBackoff)
		out.due = time.Now().Add(out.backoff)
		log.Warn("start order failure could not be reported; a later heartbeat retries it", "code", failure.Code, "error", err, "retry_in", out.backoff)
	}
	h.mu.Unlock()
}

func (h *hostAgent) dropOutcome(id string) {
	h.mu.Lock()
	delete(h.outcomes, id)
	h.mu.Unlock()
}

// registrationFor finds the registration of a project on the server of c.
func registrationFor(record remote.HostRecord, c *client.Client, projectID string) (remote.ProjectRegistration, bool) {
	for _, reg := range record.Projects {
		if config.ProjectIDForName(reg.Project) == projectID && c.ServerURL() == reg.ServerURL {
			return reg, true
		}
	}
	return remote.ProjectRegistration{}, false
}

// startArguments are the arguments of the kander start of an order.
func startArguments(order api.StartOrder) []string {
	args := []string{"start", "--json"}
	if order.Agent != "" {
		args = append(args, "--agent", order.Agent)
	}
	if order.Launcher != "" {
		args = append(args, "--launcher", order.Launcher)
	}
	return append(args, order.TaskID)
}

// startEnvironment is the environment of the kander start of an order: the
// worker's own, without the execution variables and without the managed mark,
// with the PATH of the scan, the config_home.env values the scan recorded for
// the project's agents, the client home of this worker, and the order.
func startEnvironment(home, order string, record remote.HostRecord, scan remote.ProjectScan) []string {
	extra := map[string]string{remote.EnvClientHome: home, process.EnvStartOrder: order}
	if record.Scan != nil && record.Scan.Path != "" {
		extra["PATH"] = record.Scan.Path
	}
	for _, agent := range scan.Agents {
		maps.Copy(extra, agent.Env)
	}
	var entries []string
	for _, name := range slices.Sorted(maps.Keys(extra)) {
		entries = append(entries, name+"="+extra[name])
	}
	env := process.DaemonEnvironment()
	kept := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := extra[name]; !replaced {
			kept = append(kept, entry)
		}
	}
	return append(kept, entries...)
}

// childFailure takes the code and message of the JSON error the start wrote
// on stderr, else the exit status.
func childFailure(stderr []byte, runErr error) (code, message string) {
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 0, 64<<10), startChildOutputLimit)
	for scanner.Scan() {
		var line struct {
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Error != nil && line.Error.Code != "" {
			code, message = line.Error.Code, line.Error.Message
		}
	}
	if code != "" && api.ValidIdentity(code) {
		return code, message
	}
	if runErr == nil {
		return "", ""
	}
	text := strings.TrimSpace(string(stderr))
	if text == "" {
		text = runErr.Error()
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return "start_failed", text
	}
	return "start_not_finished", text
}

// limited keeps at most n bytes of a stream and drops the rest.
type limited struct {
	w *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		l.w.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// defaultStartExecutable is the binary of this worker, resolved through
// symlinks so a replaced link still names a file.
func defaultStartExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}
