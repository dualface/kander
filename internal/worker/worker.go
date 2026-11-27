// Package worker is the per-user background process of a v2 client. It
// renews the leases of this host's claims, and when a lease is lost it stops
// the managed agent of that execution and keeps the worktree as it is. One
// worker runs per client home; it outlives the TUI and the command that
// started it, and exits once no live claim is left.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/version"
)

// idleExit is how long a worker without live claims stays before exiting.
const idleExit = 2 * time.Minute

// Options configure one worker loop; tests shorten the timings.
type Options struct {
	Observe  func(context.Context, remote.Credential) api.AgentObservation
	Home     string
	Tick     time.Duration
	IdleExit time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
	// Version and PID name this worker in worker.json and in the log; the
	// defaults are version.String and os.Getpid. Tests set them to tell a
	// worker from the one that handed over to it in the same process.
	Version func() string
	PID     func() int
	// WriteRecord publishes worker.json; the default is remote.WriteWorkerRecord.
	WriteRecord func(home string, r remote.WorkerRecord) error
	// Stop overrides process termination in tests. The default verifies the
	// recorded process incarnation before signaling its kernel handle.
	Stop func(pid int) error
	// Resolve finds the agent recorded by the card's WINDOW and SESSION and
	// returns the way to type a line and observe output; the default is Resolve.
	Resolve func(AgentAddress) (*Target, error)
	// CreateRequestFile writes a private request file and transfers ownership
	// of its directory to the worker. The default is CreateRequestFile.
	CreateRequestFile func(body string) (*fs.TempDir, string, error)
	waitForAckPoll    func(context.Context, time.Time) bool
	// The fields below drive the handover to a replaced binary; the defaults
	// are the production behavior and tests replace them to start no process.
	//
	// Executable names the file this worker started from; the default is
	// os.Executable.
	Executable func() (string, error)
	// Identity reads the identity of the file at a path; the default reads it
	// through an open handle.
	Identity func(path string) (os.FileInfo, error)
	// Probe checks that the replaced binary works; the default runs
	// `<executable> version` with a bounded timeout.
	Probe func(ctx context.Context, executable string) error
	// Spawn starts the worker of the replaced binary for a client home; the
	// default is remote.StartWorker.
	Spawn func(home, executable string) (Successor, error)
	// Held reports whether a worker holds the lock of a client home; the
	// default is remote.WorkerHeld.
	Held func(home string) (bool, error)
	// ConfirmTimeout and ConfirmPoll bound the wait for the started worker to
	// take the lock and set its pace.
	ConfirmTimeout time.Duration
	ConfirmPoll    time.Duration
}

// Prompt types one line into a resolved agent pane. An error leaves the outcome
// uncertain: the line may have reached the pane.
type Prompt func(text string) error

// Target binds prompt delivery and acknowledgement probes to the same pane.
type Target struct {
	Prompt Prompt
	Seen   func(context.Context, string) (bool, error)
	// Window is set when resolution found a different pane by session.
	Window string
}

// AgentAddress names the agent of an execution for a terminal hook: the
// WINDOW and SESSION values, and the directory whose project overlay the
// agent definition is read with. An empty ConfigDir reads the configuration
// of the worker's own directory.
type AgentAddress struct {
	Window, Session, ConfigDir string
}

// Resolve is the terminal side of the delivery of collaboration requests. It
// finds the agent pane that a card's WINDOW and SESSION name and checks that the
// pane takes a line, without typing anything: an error means nothing was typed.
// The notify package sets it at init, so this package stays free of terminal
// code; a binary without it keeps requests open for the holder to read.
var Resolve func(AgentAddress) (*Target, error)

// CreateRequestFile is supplied by notify alongside Resolve.
var CreateRequestFile func(body string) (*fs.TempDir, string, error)

// NewCommand builds `kander worker`, the internal command that holds the worker lock of the
// client home and renews until idle or signaled. It takes no arguments.
func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:    "worker",
		Hidden: true,
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(0, 0)(c, args); err != nil {
				return err
			}
			if err := command.NoJSON(); err != nil {
				return err
			}
			// Not even a global flag: the worker is started with no arguments, and any flag
			// that was given, whatever its value, is a mistake that would otherwise start it.
			var given []string
			c.Flags().Visit(func(f *pflag.Flag) { given = append(given, f.Name) })
			if len(given) > 0 {
				return command.Usage("board.unknown_option", "--"+given[0])
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error { return command.Exit(run()) },
	}
	// The command has no contract file: its help and its usage are the one line below.
	const description = "kander worker is a command Kander runs internally; it takes no arguments."
	c.SetHelpFunc(func(c *cobra.Command, _ []string) { fmt.Fprintln(c.OutOrStdout(), description) })
	c.SetUsageFunc(func(c *cobra.Command) error {
		_, err := fmt.Fprint(c.ErrOrStderr(), description)
		return err
	})
	return c
}

func run() int {
	home, err := remote.Home()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander worker:", err)
		return 1
	}
	// The text of the observations this worker reports follows its configuration.
	config.BindEffectiveLanguage()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWorker(ctx, home, os.Stderr)
}

// runWorker logs the ignored configuration once and serves until ctx ends or the worker is idle.
// It returns the exit status of the command.
func runWorker(ctx context.Context, home string, logOut io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(logOut, nil))
	logIgnoredConfig(logger)
	err := Serve(ctx, Options{Home: home, Logger: logger})
	if errors.Is(err, ErrRunning) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander worker:", err)
		return 1
	}
	return 0
}

// logIgnoredConfig writes the settings of its configuration that this version ignores to the log,
// once, when the worker starts: a worker lives for hours and would otherwise drop the settings
// of a newer version silently. It reads the scope configuration only: the worker runs in the
// client home, which may sit below a repository or a directory that holds a project overlay, and
// that overlay belongs to a project, not to this process. An unreadable configuration is logged
// without stopping the worker.
func logIgnoredConfig(logger *slog.Logger) {
	cfg, err := config.LoadScope(true)
	if err != nil {
		logger.Warn("the configuration could not be loaded; the settings this version ignores are not listed", "error", err)
		return
	}
	config.LogIgnored(logger, cfg)
}

// ErrRunning reports that another worker holds the lock.
var ErrRunning = errors.New("another worker is running")

// Serve runs the renewal loop and the work loop until ctx ends or the worker
// is idle.
func Serve(ctx context.Context, o Options) error {
	if o.Tick <= 0 {
		o.Tick = time.Second
	}
	if o.IdleExit <= 0 {
		o.IdleExit = idleExit
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Resolve == nil {
		o.Resolve = Resolve
	}
	if o.Version == nil {
		o.Version = version.String
	}
	if o.PID == nil {
		o.PID = os.Getpid
	}
	if o.WriteRecord == nil {
		o.WriteRecord = remote.WriteWorkerRecord
	}
	o.defaultHandover()
	if err := os.MkdirAll(o.Home, 0o700); err != nil {
		return err
	}
	lock := &workerLock{home: o.Home}
	if err := lock.acquire(ctx); err != nil {
		return err
	}
	defer lock.release()
	w := newLoop(o)
	defer w.closeRequestFiles()
	self, err := newHandover(o)
	if err != nil {
		o.Logger.Warn("the executable of this worker cannot be identified; handover to a replaced binary is off", "error", err)
	}
	// The record only serves diagnostics, so a failed write is logged and the
	// worker renews as usual. The record of the worker before this one is
	// removed then: left in place it would name that worker's version and pid.
	if err := o.WriteRecord(o.Home, remote.WorkerRecord{
		Version: o.Version(), PID: o.PID(), Executable: self.path(),
		Handover: self.enabled(), StartedAt: o.Now(),
	}); err != nil {
		o.Logger.Error("worker record could not be written", "error", err)
		if err := remote.RemoveWorkerRecord(o.Home); err != nil {
			o.Logger.Error("the record of the earlier worker could not be removed", "error", err)
		}
	}
	o.Logger.Info("worker started", "version", o.Version(), "pid", o.PID(), "executable", self.path())
	// Registered after the lock release, so it runs first: only the worker that
	// holds worker.lock acts on credentials.
	w.startRenewals(ctx)
	defer w.stopRenewals()
	w.startPolicyRefresh(ctx)
	defer w.stopPolicyRefresh()
	idleSince := o.Now()
	ticker := time.NewTicker(o.Tick)
	defer ticker.Stop()
	for {
		live, err := w.workRound(ctx)
		if err != nil {
			o.Logger.Error("worker pass failed", "error", err)
		}
		if live > 0 {
			idleSince = o.Now()
		} else if o.Now().Sub(idleSince) >= o.IdleExit {
			return nil
		}
		result, err := w.tryHandover(ctx, self, lock)
		if err != nil {
			return err
		}
		if result != handoverNone {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// loop holds the state of the two loops of one worker: the renewal loop in
// renew.go and the work loop below. The work loop observes, reports, delivers
// and stops agents, one credential after the other; it never renews, so no
// slow terminal can delay a renewal.
type loop struct {
	o Options
	// scan lists the credentials; tests replace it.
	scan func(home string) ([]remote.Credential, error)
	// deliveries belongs to the work loop. Each scope synchronizes the
	// acknowledgement results its observers publish.
	deliveries map[credentialKey]*deliveryScope
	// replays belongs to the work loop: the open managed answers of each
	// credential (answer_replay.go).
	replays map[credentialKey]*replayTable

	// mu guards every field below. It is held for map reads and writes only,
	// never across a call to the server or the file system.
	mu      sync.Mutex
	clients map[string]*client.Client
	// disconnected holds, per credential, the first and the latest renewal
	// attempt that reached no decision of the server since the last confirmed
	// renewal: retries are paced by the renewal interval and logged.
	disconnected map[credentialKey]outage
	// owed holds the credentials whose renewal was confirmed and whose work has
	// not run since. A credential gets one work step per confirmed renewal, so
	// nothing is observed, reported or delivered for a lease the server did not
	// confirm.
	owed map[credentialKey]struct{}
	// lostMarks holds the credentials the renewal loop marked lost and the work
	// loop has not stopped yet.
	lostMarks map[credentialKey]struct{}
	// blocked holds the executions whose request delivery failed before it typed
	// anything and has not succeeded since (delivery_block.go).
	blocked  map[credentialKey]blockState
	renewing *renewals

	// missingDirs holds, per credential, the recorded agent configuration
	// directory last found missing (agent_config.go).
	missingDirs map[credentialKey]string
	// policyHints holds, per project, the policy version the last confirmed
	// renewal answered and the work loop has not compared yet (policy.go).
	policyHints map[policyProject]policyHint
	// policyWake wakes the policy loop after a renewal recorded a hint.
	policyWake chan struct{}
	policies   *renewals
}

type outage struct{ since, last time.Time }

func newLoop(o Options) *loop {
	if o.CreateRequestFile == nil {
		o.CreateRequestFile = CreateRequestFile
	}
	if o.waitForAckPoll == nil {
		o.waitForAckPoll = waitForAckPoll
	}
	return &loop{
		o:            o,
		scan:         remote.Credentials,
		clients:      map[string]*client.Client{},
		disconnected: map[credentialKey]outage{},
		owed:         map[credentialKey]struct{}{},
		lostMarks:    map[credentialKey]struct{}{},
		blocked:      map[credentialKey]blockState{},
		deliveries:   map[credentialKey]*deliveryScope{},
		replays:      map[credentialKey]*replayTable{},
		missingDirs:  map[credentialKey]string{},
		policyHints:  map[policyProject]policyHint{},
		policyWake:   make(chan struct{}, 1),
	}
}

func (w *loop) client(c remote.Credential) (*client.Client, error) {
	key := c.ServerURL + "\x00" + c.ClientID
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing, ok := w.clients[key]; ok {
		return existing, nil
	}
	cl, err := client.New(c.ServerURL, c.ClientID)
	if err != nil {
		return nil, err
	}
	cl.Retries = 0
	w.clients[key] = cl
	return cl, nil
}

// credentials lists the credentials of the client home. The renewal loop can
// remove a released credential while the other loop is scanning, which fails
// the scan once; one retry reads the directory again.
func (w *loop) credentials() ([]remote.Credential, error) {
	all, err := w.scan(w.o.Home)
	if err == nil {
		return all, nil
	}
	w.o.Logger.Debug("credential scan retried", "error", err)
	return w.scan(w.o.Home)
}

// workRound serves every credential once and returns the number still live. It
// replaces the earlier serial pass: the renewals it used to make are the job of
// the renewal loop.
func (w *loop) workRound(ctx context.Context) (int, error) {
	started := w.o.Now()
	w.expireRequestFiles(started)
	credentials, err := w.credentials()
	if err != nil {
		return 0, err
	}
	for i := range credentials {
		w.refreshGroupContext(&credentials[i])
	}
	w.forgetAbsent(credentials)
	stopped := map[credentialKey]struct{}{}
	budget := &resolveBudget{}
	live := 0
	for _, c := range credentials {
		w.stopMarked(ctx, stopped)
		key := keyOf(c)
		if _, done := stopped[key]; done {
			continue
		}
		if c.Lost {
			w.stopLost(ctx, w.o.Logger, c)
			continue
		}
		w.flushAcknowledgements(ctx, c)
		if !w.takeOwed(key) || w.work(ctx, c, budget) {
			live++
		}
	}
	// A lease lost during the last work step is stopped in this round, not the
	// next one, which may be a handover away.
	w.stopMarked(ctx, stopped)
	w.warnSlow("work round exceeded the renewal interval", started, credentials)
	return live, nil
}

// refreshGroupContext completes a group credential with the facts of the
// execution that coordinates the group: the agent of a task that also
// coordinates its own group is recorded on the task credential only. The
// credential is written only when the completion changed it: a write of the
// same content every round would rewrite a lost credential for as long as it
// is kept.
func (w *loop) refreshGroupContext(c *remote.Credential) {
	if c.ResourceKind != "group" {
		return
	}
	before := *c
	if err := remote.RefreshExecutionContext(w.o.Home, c); err != nil {
		w.o.Logger.Error("group execution context could not be read", "group", c.TaskID, "error", err)
	} else if reflect.DeepEqual(before, *c) {
		return
	} else if err := remote.WriteCredential(w.o.Home, *c); err != nil && !errors.Is(err, remote.ErrCredentialReleased) {
		w.o.Logger.Error("group execution context could not be saved", "group", c.TaskID, "error", err)
	}
}

// takeOwed reports whether the credential has a work step owed and clears it,
// so a work step that fails halfway is not repeated before the next confirmed
// renewal.
func (w *loop) takeOwed(key credentialKey) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, owed := w.owed[key]
	delete(w.owed, key)
	return owed
}

// forgetAbsent drops the bookkeeping of credentials that no longer exist. A lost
// credential keeps its marks but loses its delivery state and its open answers:
// its agent is stopped, so nothing is delivered or answered for it any more.
func (w *loop) forgetAbsent(credentials []remote.Credential) {
	present := make(map[credentialKey]struct{}, len(credentials))
	live := make(map[credentialKey]struct{}, len(credentials))
	for _, c := range credentials {
		present[keyOf(c)] = struct{}{}
		if !c.Lost {
			live[keyOf(c)] = struct{}{}
		}
	}
	w.forgetBlocks(live)
	for key := range w.deliveries {
		if _, ok := live[key]; !ok {
			w.closeDeliveryScope(key)
		}
	}
	for key := range w.replays {
		if _, ok := live[key]; !ok {
			w.forgetReplays(key)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.owed {
		if _, ok := present[key]; !ok {
			delete(w.owed, key)
		}
	}
	for key := range w.lostMarks {
		if _, ok := present[key]; !ok {
			delete(w.lostMarks, key)
		}
	}
	for key := range w.disconnected {
		if _, ok := present[key]; !ok {
			delete(w.disconnected, key)
		}
	}
	for key := range w.missingDirs {
		if _, ok := present[key]; !ok {
			delete(w.missingDirs, key)
		}
	}
}

// stopMarked stops the agents of the credentials the renewal loop marked lost
// since the last check. The work loop runs it before every work step, so a
// lost lease waits for at most the step in progress. The credentials it stops
// are added to stopped, which keeps the copy of the credential that was read at
// the start of the round from being served again. It reports whether it had
// marks to work on and read the credentials; a failed read leaves the marks for
// the next check and reports false.
func (w *loop) stopMarked(ctx context.Context, stopped map[credentialKey]struct{}) bool {
	w.mu.Lock()
	marks := w.lostMarks
	w.lostMarks = map[credentialKey]struct{}{}
	w.mu.Unlock()
	if len(marks) == 0 {
		return false
	}
	credentials, err := w.credentials()
	if err != nil {
		w.o.Logger.Error("lost credentials could not be read; their agents are stopped later", "error", err)
		w.mu.Lock()
		for key := range marks {
			w.lostMarks[key] = struct{}{}
		}
		w.mu.Unlock()
		return false
	}
	for _, c := range credentials {
		key := keyOf(c)
		if _, marked := marks[key]; !marked || !c.Lost {
			continue
		}
		w.refreshGroupContext(&c)
		w.stopLost(ctx, w.o.Logger, c)
		stopped[key] = struct{}{}
	}
	return true
}

// marked reports whether a lease is marked lost and not stopped yet.
func (w *loop) marked() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.lostMarks) > 0
}

// warnSlow logs a round that took longer than the shortest renewal interval of
// its credentials: a lease can lapse once a round takes longer than the TTL.
func (w *loop) warnSlow(msg string, started time.Time, credentials []remote.Credential) {
	interval := 15 * time.Second
	for i, c := range credentials {
		if i == 0 || renewInterval(c) < interval {
			interval = renewInterval(c)
		}
	}
	if duration := w.o.Now().Sub(started); duration > interval {
		w.o.Logger.Warn(msg, "duration", duration, "interval", interval, "credentials", len(credentials))
	}
}

// work is the half of a credential's step that follows a confirmed renewal: it
// observes the agent, reports the observation and delivers requests. It
// reports whether the credential is still live.
func (w *loop) work(ctx context.Context, c remote.Credential, budget *resolveBudget) bool {
	log := w.credentialLog(c)
	cl, err := w.client(c)
	if err != nil {
		log.Warn("worker client unavailable", "error", err)
		return true
	}
	if c.ResourceKind == "integration" {
		return w.integrationWork(ctx, log, c)
	}
	project := cl.Project(c.ProjectID)
	observeCtx, stopObservation := context.WithTimeout(ctx, 2*time.Second)
	observation := w.observe(observeCtx, c)
	stopObservation()
	if observation.Status != "unknown" {
		if c.ResourceKind != "group" {
			observation.Reason = w.withBlockedSuffix(keyOf(c), observation.Reason)
		}
		reportCtx, stopReport := context.WithTimeout(ctx, 5*time.Second)
		var reportErr error
		if c.ResourceKind == "group" {
			_, reportErr = project.ObserveGroup(reportCtx, client.NewKey(), c.TaskID, observation, *c.Claim())
		} else {
			_, reportErr = project.ObserveAgent(reportCtx, client.NewKey(), c.TaskID, observation, *c.Claim())
		}
		stopReport()
		if reportErr != nil {
			log.Warn("agent observation report failed", "error", reportErr)
		}
		if c.ResourceKind == "group" && observation.Status == "stopped" {
			w.lose(log, c, "agent stopped: "+observation.Reason)
			return false
		}
	}
	if c.ResourceKind != "group" && observation.Status != "stopped" {
		// The renewal runs in another loop, so a slow delivery cannot cost a
		// lease; each server call of the delivery has a time limit of its own.
		w.deliver(ctx, log, project, &c, budget)
	}
	if err := remote.WriteCredential(w.o.Home, c); errors.Is(err, remote.ErrCredentialReleased) {
		// The renewal loop removed the credential while this step ran.
		log.Debug("claim released during the work step")
	} else if err != nil {
		log.Error("credential write failed", "error", err)
	}
	return true
}

// lose marks the credential lost and stops the managed agent. The worktree,
// branch, and uncommitted changes are left untouched for manual handling. Only
// the work loop calls it: the renewal loop marks a lease lost and leaves the
// stop to the work loop.
func (w *loop) lose(log *slog.Logger, c remote.Credential, reason string) {
	if !w.markLost(log, c, reason) {
		return
	}
	c.Lost, c.LostReason = true, reason
	w.stopLost(context.Background(), log, c)
	w.mu.Lock()
	delete(w.lostMarks, keyOf(c))
	w.mu.Unlock()
}
