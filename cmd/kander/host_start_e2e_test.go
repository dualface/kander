package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
// the configuration file; each has its own checkout and client identity.
type twoHosts struct {
	t              *testing.T
	url            string
	state          string
	homeA, homeB   string
	checkA, checkB string
	clientB        string
	workerB        *runningWorker
	path           string
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
	for name, text := range map[string]string{"fakemuxctl": fakeMuxScript, "claude": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
	t.Setenv("FAKEMUX_STATE", state)
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
	httpServer := httptest.NewServer(srv.Handler())
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
	h := &twoHosts{t: t, url: httpServer.URL, state: state, path: path,
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
	// The scan of host B: the fake agent and the fake terminal, with this PATH.
	if err := remote.StoreHostScan(h.homeB, remote.HostScan{ScannedAt: time.Now(), Path: path, Projects: []remote.ProjectScan{{
		ServerURL: h.url, Project: "two-hosts", Agents: []remote.AgentScan{{Name: "claude", Path: filepath.Join(bin, "claude")}}, Launchers: []string{"fakemux"},
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
		w.done <- worker.Serve(ctx, worker.Options{Home: home, Tick: 50 * time.Millisecond, Logger: logger, Managed: true, AcceptOrders: orders, HostRescan: 100 * time.Millisecond, HeartbeatWait: 2 * time.Second})
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

	// The same order run again finds its launch receipt: no second agent.
	cmd := exec.Command(os.Args[0], "start", "--json", id)
	cmd.Dir = h.checkB
	cmd.Env = append(os.Environ(), remote.EnvClientHome+"="+h.homeB, process.EnvStartOrder+"="+order.ID)
	out, err := cmd.CombinedOutput()
	if err != nil || h.panes() != 1 {
		t.Fatalf("second run of the order: %v panes=%d\n%s", err, h.panes(), out)
	}

	// The worker stops and its agent keeps running.
	h.workerB.stop(t)
	if dead, err := os.ReadFile(filepath.Join(h.state, "panes", "p1", "dead")); err != nil || string(dead) != "0" {
		t.Fatalf("the agent did not survive the worker: %q %v", dead, err)
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
