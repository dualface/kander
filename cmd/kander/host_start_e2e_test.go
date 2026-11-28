package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install/installtest"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

// cliChildEnv makes this test binary run the kander command line it is given:
// the worker of a host runs `kander start` through it.
const cliChildEnv = "KANDER_TEST_CLI_CHILD"

// twoHosts is one server and two client homes on this machine: host A asks,
// host B runs the start. Both hosts share the fake terminal, the remote, and
// the configuration file; each has its own checkout and client identity. The
// fake terminal runs the agent as a long-lived process with a clean
// environment, and the server sits behind faults that tests switch on.
type twoHosts struct {
	t              *testing.T
	url            string
	state          string
	homeA, homeB   string
	checkA, checkB string
	clientB        string
	workerB        *runningWorker
	path           string
	configDir      string
	heartbeatWait  time.Duration
	faults         *serverFaults
}

// serverFaults stands in front of the test server. oldServer answers the host
// and start order routes with a plain 404, as a server without them does;
// failReports makes the gateway in front of the server answer the next failure
// reports of a start order with 502 before they reach the server.
type serverFaults struct {
	next        http.Handler
	oldServer   atomic.Bool
	failReports atomic.Int32
	dropped     atomic.Int32
}

func (f *serverFaults) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.oldServer.Load() && (strings.HasPrefix(r.URL.Path, api.HostsRoute) || strings.Contains(r.URL.Path, "/start-orders")) {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/fail") && f.failReports.Add(-1) >= 0 {
		f.dropped.Add(1)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	f.next.ServeHTTP(w, r)
}

// testAgent is the agent process the fake terminal started in a pane.
type testAgent struct {
	pid int
	env map[string]string
}

type runningWorker struct {
	cancel context.CancelFunc
	done   chan error
	log    *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func hostGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Kander Test", "-c", "user.email=kander@example.invalid", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newTwoHosts(t *testing.T) *twoHosts {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake terminal is a POSIX shell script")
	}
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Setenv("TMPDIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	installtest.IsolateAgentConfigEnv(t)
	shareDir := filepath.Join(home, ".local", "share", "kander", terminal.DefinitionsDirName)
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The worker starts from a process without a terminal, so the launcher declares it may.
	definition := strings.Replace(fakeMuxDefinition, `"fakemux": {`, `"fakemux": {"background": true,`, 1)
	if err := os.WriteFile(filepath.Join(shareDir, "fakemux.json"), []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	terminal.ReloadDefinitions()
	t.Cleanup(terminal.ReloadDefinitions)
	bin, state := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(state, "panes"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The agent records its environment and stays: a pane starts it, any other caller ends at once.
	agentScript := "#!/bin/sh\n[ -n \"$FAKEMUX_PANE\" ] || exit 0\nenv > \"$FAKEMUX_PANE/agent.env\"\nprintf '%s' \"$$\" > \"$FAKEMUX_PANE/agent.pid\"\nexec sleep 600\n"
	t.Cleanup(func() { killTestAgents(state) })
	for name, text := range map[string]string{"fakemuxctl": fakeMuxScript, "claude": agentScript} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
	t.Setenv("FAKEMUX_STATE", state)
	t.Setenv("FAKEMUX_RUN", "1")
	for _, name := range []string{"TMUX", "TMUX_PANE", "HERDR_ENV", remote.EnvExecutionID, remote.EnvCoordinatorExecutionID, process.EnvStartOrder} {
		t.Setenv(name, "")
	}
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	t.Setenv(cliChildEnv, "1")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), TTL: 30 * time.Second, RenewInterval: time.Second, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	faults := &serverFaults{next: srv.Handler()}
	httpServer := httptest.NewServer(faults)
	t.Cleanup(httpServer.Close)

	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	hostGit(t, root, "init", "-q", "--bare", bare)
	hostGit(t, root, "clone", "-q", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "work.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostGit(t, seed, "add", "work.txt")
	hostGit(t, seed, "commit", "-q", "-m", "Initial work")
	hostGit(t, seed, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/develop")
	h := &twoHosts{t: t, url: httpServer.URL, state: state, path: path, configDir: filepath.Join(root, "claude-config-b"), heartbeatWait: 2 * time.Second, faults: faults,
		homeA: filepath.Join(root, "home-a"), homeB: filepath.Join(root, "home-b"),
		checkA: filepath.Join(root, "checkout-a"), checkB: filepath.Join(root, "checkout-b"),
		clientB: "c-00000000000b-hostb"}
	hostGit(t, root, "clone", "-q", "--branch", "develop", bare, h.checkA)
	hostGit(t, root, "clone", "-q", "--branch", "develop", bare, h.checkB)
	for home, id := range map[string]string{h.homeA: "c-00000000000a-hosta", h.homeB: h.clientB} {
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "client-id"), []byte(id+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv(config.EnvConfig, filepath.Join(home, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language, cfg.AgentLanguage = "en", "en"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	cfg.Launcher = "fakemux"
	// The fixture commits no project rules files, so the start skips their check.
	cfg.InstallRules = false
	renameDisabled := ""
	cfg.Agents = map[string]config.AgentDefinition{"claude": {RenameCommand: &renameDisabled}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// Host A holds its worker lock with a worker that takes no orders, so no
	// command of the test spawns this binary as a background worker.
	h.serveWorker(h.homeA, false)
	h.as("a")
	runKander(t, "init", "--server", h.url, "--project", "two-hosts").require(t, "init on host A")
	h.workerB = h.serveWorker(h.homeB, true)
	h.as("b")
	runKander(t, "init", "--server", h.url, "--project", "two-hosts").require(t, "init on host B")
	// The scan of host B: the fake agent with its own configuration directory
	// and the fake terminal, with this PATH.
	if err := remote.StoreHostScan(h.homeB, remote.HostScan{ScannedAt: time.Now(), Path: path, Projects: []remote.ProjectScan{{
		ServerURL: h.url, Project: "two-hosts", Agents: []remote.AgentScan{{Name: "claude", Path: filepath.Join(bin, "claude"), Env: map[string]string{"CLAUDE_CONFIG_DIR": h.configDir}}}, Launchers: []string{"fakemux"},
	}}}); err != nil {
		t.Fatal(err)
	}
	h.as("a")
	h.waitOnline(true)
	return h
}

// as makes the commands of the test run as host a or b: its client home and checkout.
func (h *twoHosts) as(host string) {
	h.t.Helper()
	home, dir := h.homeA, h.checkA
	if host == "b" {
		home, dir = h.homeB, h.checkB
	}
	h.t.Setenv(remote.EnvClientHome, home)
	h.t.Chdir(dir)
}

func (h *twoHosts) serveWorker(home string, orders bool) *runningWorker {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &runningWorker{cancel: cancel, done: make(chan error, 1), log: &syncBuffer{}}
	logger := slog.New(slog.NewTextHandler(w.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	go func() {
		w.done <- worker.Serve(ctx, worker.Options{Home: home, Tick: 50 * time.Millisecond, Logger: logger, Managed: true, AcceptOrders: orders, HostRescan: 100 * time.Millisecond, HeartbeatWait: h.heartbeatWait})
	}()
	h.t.Cleanup(func() { w.stop(h.t) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if held, _ := remote.WorkerHeld(home); held {
			return w
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatal("worker lock was not acquired")
	return nil
}

func (w *runningWorker) stop(t *testing.T) {
	if w.cancel == nil {
		return
	}
	w.cancel()
	w.cancel = nil
	if err := <-w.done; err != nil {
		t.Errorf("worker: %v", err)
	}
}

func (h *twoHosts) project() *client.Project {
	h.t.Helper()
	session := currentCommandSession(h.t)
	return session.Project.(*client.Project)
}

func (h *twoHosts) hostB(t *testing.T) (*client.Client, *client.Project) {
	t.Helper()
	c, err := client.New(h.url, h.clientB)
	if err != nil {
		t.Fatal(err)
	}
	return c, c.Project(h.project().ID())
}

func (h *twoHosts) waitOnline(online bool) {
	h.t.Helper()
	c, err := client.New(h.url, "c-00000000000a-hosta")
	if err != nil {
		h.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list, err := c.Hosts(h.t.Context())
		if err == nil {
			for _, host := range list.Hosts {
				if host.Name == "hostb" && host.Online == online && len(host.Projects) == 1 && len(host.Projects[0].Launchers) == 1 {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("host B is not reported online with its project; worker log:\n%s", h.workerB.log.String())
}

func (h *twoHosts) panes() int {
	entries, _ := os.ReadDir(filepath.Join(h.state, "panes"))
	return len(entries)
}

// agent waits for the agent process of a pane and reads its environment.
func (h *twoHosts) agent(pane string) testAgent {
	h.t.Helper()
	dir := filepath.Join(h.state, "panes", pane)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(dir, "agent.pid")); err == nil && len(data) > 0 {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				h.t.Fatalf("agent pid %q: %v", data, err)
			}
			text, err := os.ReadFile(filepath.Join(dir, "agent.env"))
			if err != nil {
				h.t.Fatal(err)
			}
			env := map[string]string{}
			for _, line := range strings.Split(string(text), "\n") {
				if name, value, ok := strings.Cut(line, "="); ok {
					env[name] = value
				}
			}
			return testAgent{pid: pid, env: env}
		}
		time.Sleep(20 * time.Millisecond)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "agent.out"))
	h.t.Fatalf("no agent process in pane %s; output: %s", pane, out)
	return testAgent{}
}

// requireAlive fails unless the agent process still runs and still is the agent.
func requireAlive(t *testing.T, agent testAgent, when string) {
	t.Helper()
	out, err := exec.Command("ps", "-o", "stat=", "-o", "args=", "-p", strconv.Itoa(agent.pid)).Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(out)), "Z") || !strings.Contains(string(out), "sleep 600") {
		t.Fatalf("the agent process %d does not run %s: %q %v", agent.pid, when, out, err)
	}
}

// killTestAgents ends the agent processes the fake terminal started.
func killTestAgents(state string) {
	pids, _ := filepath.Glob(filepath.Join(state, "panes", "*", "agent.pid"))
	for _, file := range pids {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if pid, err := strconv.Atoi(string(data)); err == nil {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	}
}

// waitWorker waits until the server saw a heartbeat of a newly started worker.
func (h *twoHosts) waitWorker(w *runningWorker) {
	h.t.Helper()
	c, err := client.New(h.url, "c-00000000000a-hosta")
	if err != nil {
		h.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		id := ""
		if _, after, ok := strings.Cut(w.log.String(), "worker_id="); ok {
			id, _, _ = strings.Cut(after, "\n")
		}
		if list, err := c.Hosts(h.t.Context()); err == nil && id != "" {
			for _, host := range list.Hosts {
				if host.Name == "hostb" && host.WorkerID == id {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("the new worker sent no heartbeat; worker log:\n%s", w.log.String())
}

func decodeOrder(t *testing.T, r commandResult) api.StartOrder {
	t.Helper()
	var order api.StartOrder
	if err := json.Unmarshal([]byte(r.stdout), &order); err != nil {
		t.Fatalf("not an order: %v\nstdout=%s\nstderr=%s", err, r.stdout, r.stderr)
	}
	return order
}

// HostA asks HostB to start a card: B's worker takes the order, runs kander
// start in its registered checkout, and the order reports the started agent.
func TestStartOnAnotherHostRunsTheStartThere(t *testing.T) {
	h := newTwoHosts(t)
	hosts := decodeHosts(t, runKander(t, "host", "list", "--json").require(t, "host list"))
	if len(hosts.Hosts) != 1 || hosts.Hosts[0].Name != "hostb" || !hosts.Hosts[0].Online || hosts.Hosts[0].Projects[0].Agents[0] != "claude" {
		t.Fatalf("hosts = %+v", hosts)
	}

	id := readyCard(t, "on-host-b")
	result := runKander(t, "start", "--host", "hostb", "--timeout", "60", "--json", id)
	order := decodeOrder(t, result)
	if result.code != 0 || order.State != api.StartOrderStarted || order.Generation != 1 || order.Window != "fakemux:w1:p1" || order.TargetHost != "hostb" {
		t.Fatalf("exit=%d order=%+v\nstderr=%s\nworker log:\n%s", result.code, order, result.stderr, h.workerB.log.String())
	}
	task, err := h.project().Show(t.Context(), id)
	if err != nil || task.State != "working" || task.Claim == nil || task.Claim.ClientID != h.clientB {
		t.Fatalf("task = %+v %v", task, err)
	}
	shown := runKander(t, "order", "show", "--json", order.ID).require(t, "order show")
	if decodeOrder(t, shown).State != api.StartOrderStarted {
		t.Fatalf("order show: %s", shown.stdout)
	}

	// The agent got the scanned PATH and configuration directory through the
	// pane command; the fake terminal itself passes it a clean environment.
	agent := h.agent("p1")
	if agent.env["PATH"] != h.path || agent.env["CLAUDE_CONFIG_DIR"] != h.configDir {
		t.Fatalf("agent environment PATH=%q CLAUDE_CONFIG_DIR=%q, want %q and %q", agent.env["PATH"], agent.env["CLAUDE_CONFIG_DIR"], h.path, h.configDir)
	}

	// The same order run again finds its launch receipt: no second agent.
	cmd := exec.Command(os.Args[0], "start", "--json", id)
	cmd.Dir = h.checkB
	cmd.Env = append(os.Environ(), remote.EnvClientHome+"="+h.homeB, process.EnvStartOrder+"="+order.ID)
	out, err := cmd.CombinedOutput()
	if err != nil || h.panes() != 1 {
		t.Fatalf("second run of the order: %v panes=%d\n%s", err, h.panes(), out)
	}

	// The worker stops and comes back; the same agent process runs throughout,
	// and no second agent starts.
	h.workerB.stop(t)
	requireAlive(t, agent, "after the worker stopped")
	h.workerB = h.serveWorker(h.homeB, true)
	h.waitWorker(h.workerB)
	requireAlive(t, agent, "after the worker restarted")
	if h.panes() != 1 || h.agent("p1").pid != agent.pid {
		t.Fatalf("the restarted worker started another agent: panes=%d", h.panes())
	}
}

func decodeHosts(t *testing.T, r commandResult) api.HostList {
	t.Helper()
	var list api.HostList
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil {
		t.Fatalf("not a host list: %v\n%s", err, r.stdout)
	}
	return list
}

// An order nobody takes leaves kander start --host at its timeout with the
// order still open; the order can be cancelled. A direct start on another host
// ends an open order, and a worker that comes back never starts it.
func TestStartOrderTimeoutCancelAndPreemption(t *testing.T) {
	h := newTwoHosts(t)
	// Host B goes quiet: its last heartbeat keeps it online for a minute.
	h.workerB.stop(t)

	id := readyCard(t, "waits")
	result := runKander(t, "start", "--host", "hostb", "--timeout", "1", "--json", id)
	order := decodeOrder(t, result)
	if result.code != 3 || order.State != api.StartOrderPending {
		t.Fatalf("timeout: exit=%d order=%+v stderr=%s", result.code, order, result.stderr)
	}
	cancelled := decodeOrder(t, runKander(t, "order", "cancel", "--json", order.ID).require(t, "order cancel"))
	if cancelled.State != api.StartOrderCancelled {
		t.Fatalf("cancel = %+v", cancelled)
	}
	again := runKander(t, "start", "--host", "hostb", "--timeout", "1", id)
	if again.code != 3 || !strings.Contains(again.stderr, "kander order show") {
		t.Fatalf("text timeout: exit=%d stderr=%s", again.code, again.stderr)
	}

	// The second order is still pending; a start on host A claims the card first.
	runKander(t, "start", "--launcher", "fakemux", id).require(t, "direct start on host A")
	_, projectB := h.hostB(t)
	second := strings.TrimSuffix(strings.Fields(strings.SplitN(again.stderr, "Start order ", 2)[1])[0], ":")
	orders, err := projectB.StartOrder(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	if orders.State != api.StartOrderFailed || orders.Reason == nil || orders.Reason.Code != api.OrderReasonAlreadyClaimed {
		t.Fatalf("preempted order = %+v", orders)
	}
	before := h.panes()
	h.workerB = h.serveWorker(h.homeB, true)
	h.waitOnline(true)
	time.Sleep(500 * time.Millisecond)
	if h.panes() != before {
		t.Fatalf("host B started a preempted order: panes %d -> %d", before, h.panes())
	}
}

// A worker that took an order and stopped before its start ran runs the start
// when it comes back.
func TestWorkerRestartRunsAnAcceptedOrder(t *testing.T) {
	h := newTwoHosts(t)
	h.workerB.stop(t)
	id := readyCard(t, "recover")
	order, err := h.project().CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: id, Host: "hostb"})
	if err != nil {
		t.Fatal(err)
	}
	_, projectB := h.hostB(t)
	if order, err = projectB.AcceptStartOrder(t.Context(), "", order.ID); err != nil || order.State != api.StartOrderAccepted {
		t.Fatalf("accept as host B: %+v %v", order, err)
	}
	h.workerB = h.serveWorker(h.homeB, true)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		current, err := projectB.StartOrder(t.Context(), order.ID)
		if err == nil && api.StartOrderFinal(current.State) {
			if current.State != api.StartOrderStarted || h.panes() != 1 {
				t.Fatalf("recovered order = %+v panes=%d\nworker log:\n%s", current, h.panes(), h.workerB.log.String())
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the accepted order did not start after the restart\nworker log:\n%s", h.workerB.log.String())
}

// The server refuses an order the target host cannot take, before it creates one.
func TestStartOnAnotherHostRefusals(t *testing.T) {
	h := newTwoHosts(t)
	id := readyCard(t, "refused")
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--host", "nohost"}, api.CodeHostUnknown},
		{[]string{"--host", "hostb", "--agent", "codex"}, api.CodeHostAgentUnavailable},
		{[]string{"--host", "hostb", "--launcher", "tmux-session"}, api.CodeHostLauncherUnavailable},
	} {
		result := runKander(t, append(append([]string{"start", "--json"}, c.args...), id)...)
		if result.code != 1 || !strings.Contains(result.stderr, `"code":"`+c.code+`"`) {
			t.Fatalf("%v: exit=%d stderr=%s", c.args, result.code, result.stderr)
		}
	}
	// The own host name starts here, without an order.
	h.as("b")
	result := runKander(t, "start", "--host", "hostb", "--json", id)
	if result.code != 0 || strings.Contains(result.stdout, `"target_host"`) || !strings.Contains(result.stdout, `"action":"start"`) {
		t.Fatalf("own host: exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

// An idle worker waits in its heartbeat's long poll; a new order ends that
// wait, and the worker takes the order within five seconds.
func TestIdleWorkerTakesAnOrderWithinFiveSeconds(t *testing.T) {
	h := newTwoHosts(t)
	h.workerB.stop(t)
	h.heartbeatWait = api.HostHeartbeatMaxWait
	h.workerB = h.serveWorker(h.homeB, true)
	h.waitWorker(h.workerB)
	time.Sleep(500 * time.Millisecond)

	id := readyCard(t, "prompt")
	created := time.Now()
	order, err := h.project().CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: id, Host: "hostb"})
	if err != nil {
		t.Fatal(err)
	}
	_, projectB := h.hostB(t)
	for order.State == api.StartOrderPending {
		if time.Since(created) > 5*time.Second {
			t.Fatalf("the order is still pending after %s\nworker log:\n%s", time.Since(created), h.workerB.log.String())
		}
		time.Sleep(20 * time.Millisecond)
		if order, err = projectB.StartOrder(t.Context(), order.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("taken %s after its creation, now %s", time.Since(created), order.State)
	// The start finishes before the test ends.
	deadline := time.Now().Add(60 * time.Second)
	for !api.StartOrderFinal(order.State) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if order, err = projectB.StartOrder(t.Context(), order.ID); err != nil {
			t.Fatal(err)
		}
	}
	if order.State != api.StartOrderStarted {
		t.Fatalf("order = %+v\nworker log:\n%s", order, h.workerB.log.String())
	}
}

// A start that fails before its claim fails its order even when the report is
// lost on the wire: the worker keeps the failure and reports it again with a
// later heartbeat, so the order does not stay accepted while the host is online.
func TestLostFailureReportIsRetried(t *testing.T) {
	h := newTwoHosts(t)
	h.workerB.stop(t)
	id := readyCard(t, "lost-report")
	order, err := h.project().CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: id, Host: "hostb"})
	if err != nil {
		t.Fatal(err)
	}
	_, projectB := h.hostB(t)
	if order, err = projectB.AcceptStartOrder(t.Context(), "", order.ID); err != nil || order.State != api.StartOrderAccepted {
		t.Fatalf("accept as host B: %+v %v", order, err)
	}
	// The checkout of host B no longer binds the project, so the worker fails
	// the order before it runs kander start; the gateway fails its first two
	// reports.
	overlay := filepath.Join(h.checkB, config.OverlayFilename)
	if err := os.WriteFile(overlay, []byte(`{"server":{"url":"`+h.url+`"},"project":{"name":"elsewhere"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.faults.failReports.Store(2)
	h.workerB = h.serveWorker(h.homeB, true)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		current, err := projectB.StartOrder(t.Context(), order.ID)
		if err == nil && api.StartOrderFinal(current.State) {
			if current.State != api.StartOrderFailed || current.Reason == nil || current.Reason.Code != "registration_invalid" || h.faults.dropped.Load() != 2 {
				t.Fatalf("order = %+v dropped=%d\nworker log:\n%s", current, h.faults.dropped.Load(), h.workerB.log.String())
			}
			if retries := strings.Count(h.workerB.log.String(), "a later heartbeat retries it"); retries != 2 {
				t.Fatalf("%d retries logged\nworker log:\n%s", retries, h.workerB.log.String())
			}
			if h.panes() != 0 {
				t.Fatalf("a failed order started an agent: panes=%d", h.panes())
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the order did not end after lost failure reports\nworker log:\n%s", h.workerB.log.String())
}

// A server without hosts and start orders answers their routes with a plain
// 404: every command of them says to upgrade the server. A server with them
// still reports an unknown order as not_found.
func TestCommandsOnAServerWithoutStartOrders(t *testing.T) {
	h := newTwoHosts(t)
	id := readyCard(t, "old-server")
	h.faults.oldServer.Store(true)
	for _, args := range [][]string{
		{"host", "list", "--json"},
		{"order", "show", "--json", "so-0123456789abcdef"},
		{"order", "cancel", "--json", "so-0123456789abcdef"},
		{"start", "--host", "hostb", "--json", id},
	} {
		result := runKander(t, args...)
		if result.code != 1 || !strings.Contains(result.stderr, `"code":"start_order_unsupported"`) || !strings.Contains(result.stderr, "upgrade the server") {
			t.Fatalf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
	text := runKander(t, "host", "list")
	if text.code != 1 || !strings.Contains(text.stderr, "kander host") {
		t.Fatalf("text: exit=%d stderr=%s", text.code, text.stderr)
	}
	h.faults.oldServer.Store(false)
	result := runKander(t, "order", "show", "--json", "so-0123456789abcdef")
	if result.code != 1 || !strings.Contains(result.stderr, `"code":"not_found"`) {
		t.Fatalf("unknown order: exit=%d stderr=%s", result.code, result.stderr)
	}
}
