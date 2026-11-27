package install

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

// fakeBackground is the local server and worker a test pretends to run. Stopping one records
// its kind; a kind in fail fails.
type fakeBackground struct {
	home, dataDir string
	stopped       []string
	fail          map[string]error
	leases        []UninstallLease
}

func (f *fakeBackground) inspect(context.Context) UninstallBackground {
	stop := func(kind string) func(context.Context) error {
		return func(context.Context) error {
			f.stopped = append(f.stopped, kind)
			return f.fail[kind]
		}
	}
	return UninstallBackground{
		Processes: []UninstallProcess{
			{Kind: UninstallServer, PID: 101, Where: "http://127.0.0.1:7878", Stop: stop(UninstallServer)},
			{Kind: UninstallWorker, PID: 202, Where: f.home, Stop: stop(UninstallWorker)},
		},
		Leases:        f.leases,
		ClientHome:    f.home,
		ServerDataDir: f.dataDir,
	}
}

// uninstallFixture installs Kander into a test home, copies a kander binary into the global
// bin directory, and replaces the parts of uninstall that would reach real processes and
// binaries. writes collects every path the uninstall file layer changes.
type uninstallFixture struct {
	home    string
	paths   config.InstallPaths
	entry   string
	bg      *fakeBackground
	writes  []string
	version string
}

func newUninstallFixture(t *testing.T) *uninstallFixture {
	t.Helper()
	home := setupInstallHome(t)
	paths := installForUninstall(t, home)
	f := &uninstallFixture{home: home, paths: paths, version: "kander 2.0.0"}
	f.bg = &fakeBackground{home: filepath.Join(paths.ShareDir, "client"), dataDir: filepath.Join(home, ".local", "share", "kander-server")}
	f.entry = filepath.Join(paths.BinDir, binaryName())
	writeIntegrateFile(t, f.entry, "binary")
	// The preserved directories hold data uninstall must neither change nor delete.
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	writeIntegrateFile(t, filepath.Join(f.bg.home, "client-id"), "c-host\n")
	writeIntegrateFile(t, filepath.Join(f.bg.dataDir, "kanban", "board.json"), "{}\n")
	saved := []any{inspectUninstallBackground, uninstallVersion, uninstallBrewEntry, uninstallPackaged, uninstallWriteObserver, uninstallTerminal}
	inspectUninstallBackground = f.bg.inspect
	uninstallVersion = func(path string) (string, error) {
		if path != f.entry {
			t.Errorf("probed %s, want the global entry", path)
		}
		return f.version, nil
	}
	uninstallBrewEntry = func() string { return "" }
	uninstallPackaged = func() bool { return false }
	uninstallWriteObserver = func(path string) { f.writes = append(f.writes, path) }
	uninstallTerminal = func() bool { return false }
	t.Cleanup(func() {
		inspectUninstallBackground = saved[0].(func(context.Context) UninstallBackground)
		uninstallVersion = saved[1].(func(string) (string, error))
		uninstallBrewEntry = saved[2].(func() string)
		uninstallPackaged = saved[3].(func() bool)
		uninstallWriteObserver, _ = saved[4].(func(string))
		uninstallTerminal = saved[5].(func() bool)
	})
	return f
}

// preserved are the data directories of the fixture.
func (f *uninstallFixture) preserved() []string {
	return []string{filepath.Dir(f.paths.ConfigPath), f.paths.ShareDir, f.bg.dataDir}
}

func TestUninstallPlanListsEverything(t *testing.T) {
	f := newUninstallFixture(t)
	f.bg.leases = []UninstallLease{
		{ProjectID: "p-1", TaskID: "20261127-a-task", Kind: "task", Generation: 2, AgentPID: 909},
		{ProjectID: "p-1", TaskID: "20261127-g-group", Kind: "group", Generation: 1},
	}
	text := planText(PlanUninstall(context.Background(), f.paths))
	for _, want := range []string{
		config.Text("install.uninstall_plan_stop", config.Text("install.uninstall_process_server", "http://127.0.0.1:7878", "101")),
		config.Text("install.uninstall_plan_stop", config.Text("install.uninstall_process_worker", f.bg.home, "202")),
		config.Text("install.uninstall_plan_remove", f.entry),
		config.Text("install.uninstall_plan_keep", filepath.Dir(f.paths.ConfigPath), config.Text("install.uninstall_keep_config")),
		config.Text("install.uninstall_plan_keep", f.paths.ShareDir, config.Text("install.uninstall_keep_share")),
		config.Text("install.uninstall_plan_keep", f.bg.dataDir, config.Text("install.uninstall_keep_server")),
		config.Text("install.uninstall_lease_agent", "p-1", "20261127-a-task", 2, "task", 909),
		config.Text("install.uninstall_lease", "p-1", "20261127-g-group", 1, "group"),
		config.Text("install.uninstall_lease_impact"),
		config.Text("install.uninstall_kept_repositories"),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plan lacks %q:\n%s", want, text)
		}
	}
}

func TestUninstallStopsDeletesAndKeepsTheData(t *testing.T) {
	f := newUninstallFixture(t)
	f.bg.leases = []UninstallLease{{ProjectID: "p-1", TaskID: "20261127-a-task", Kind: "task", Generation: 2}}
	config0 := readBytes(t, f.paths.ConfigPath)
	code, stdout, _ := commandtest.Capture(func() int { return commandtest.Run(NewUninstallCommand(), "--yes") })
	if code != 0 {
		t.Fatalf("code=%d\n%s", code, stdout)
	}
	// Leases in use do not refuse the uninstall; both processes are stopped in plan order.
	if strings.Join(f.bg.stopped, ",") != "server,worker" {
		t.Fatalf("stopped=%v", f.bg.stopped)
	}
	for _, gone := range []string{f.entry, f.paths.RulesDir} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s kept: %v", gone, err)
		}
	}
	if got := readBytes(t, f.paths.ConfigPath); got != config0 {
		t.Fatalf("config.json changed: %q", got)
	}
	for _, path := range f.writes {
		for _, dir := range f.preserved() {
			if within(path, dir) {
				t.Errorf("uninstall changed %s inside the preserved %s", path, dir)
			}
		}
	}
	if last := f.writes[len(f.writes)-1]; last != f.entry {
		t.Errorf("the binary is not deleted last: %v", f.writes)
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout), config.Text("install.uninstall_kept_repositories")) {
		t.Errorf("output does not end with what stays:\n%s", stdout)
	}
}

func TestUninstallGoesOnAfterAFailedStop(t *testing.T) {
	f := newUninstallFixture(t)
	f.bg.fail = map[string]error{UninstallServer: errors.New("server stuck")}
	code, stdout, stderr := commandtest.Capture(func() int { return commandtest.Run(NewUninstallCommand(), "--yes") })
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	if strings.Join(f.bg.stopped, ",") != "server,worker" {
		t.Fatalf("stopped=%v", f.bg.stopped)
	}
	if _, err := os.Lstat(f.entry); !os.IsNotExist(err) {
		t.Fatalf("the binary stayed after a failed stop: %v", err)
	}
	server := config.Text("install.uninstall_process_server", "http://127.0.0.1:7878", "101")
	if !strings.Contains(stdout, config.Text("install.uninstall_stop_failed", server, "server stuck")) || !strings.Contains(stderr, "server stuck") {
		t.Fatalf("failure not reported:\n%s\n%s", stdout, stderr)
	}
}

func TestUninstallRefusesWithoutConfirmation(t *testing.T) {
	f := newUninstallFixture(t)
	code, _, stderr := commandtest.Capture(func() int { return commandtest.Run(NewUninstallCommand(), "--json") })
	if code != 2 || !strings.Contains(stderr, "interactive_required") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if code := commandtest.Run(NewUninstallCommand()); code != 2 {
		t.Fatalf("no terminal without --yes: code=%d", code)
	}
	if len(f.bg.stopped) != 0 || len(f.writes) != 0 {
		t.Fatalf("a refused uninstall changed something: stopped=%v writes=%v", f.bg.stopped, f.writes)
	}
	if _, err := os.Stat(f.entry); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallJSONResult(t *testing.T) {
	f := newUninstallFixture(t)
	f.bg.fail = map[string]error{UninstallWorker: errors.New("lock held")}
	f.bg.leases = []UninstallLease{{ProjectID: "p-1", TaskID: "20261127-a-task", Kind: "task", Generation: 2}}
	code, stdout, _ := commandtest.Capture(func() int { return commandtest.Run(NewUninstallCommand(), "--json", "--yes") })
	if code != 1 {
		t.Fatalf("code=%d\n%s", code, stdout)
	}
	var result struct {
		Items    []uninstallItem  `json:"items"`
		Leases   []UninstallLease `json:"leases"`
		Failures []string         `json:"failures"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	actions := map[string]int{}
	for _, item := range result.Items {
		actions[item.Action]++
		if item.Action == "stop" && (item.Process == "" || item.PID == 0 || item.Path != "") {
			t.Errorf("stop item=%+v", item)
		}
		if item.Action != "stop" && item.Path == "" {
			t.Errorf("item without a path: %+v", item)
		}
	}
	for _, action := range []string{"stop", "remove", "edit", "keep"} {
		if actions[action] == 0 {
			t.Errorf("no %s item: %+v", action, result.Items)
		}
	}
	if len(result.Leases) != 1 || len(result.Failures) != 1 || !strings.Contains(result.Failures[0], "lock held") {
		t.Fatalf("leases=%+v failures=%v", result.Leases, result.Failures)
	}
}

func TestUninstallBinaryOwnership(t *testing.T) {
	f := newUninstallFixture(t)
	reasons := func() map[string]UninstallItem {
		out := map[string]UninstallItem{}
		for _, item := range planBinaryRemoval(f.paths) {
			out[item.Path] = item
		}
		return out
	}

	f.version = "something else 1.0"
	if item := reasons()[f.entry]; item.Action != UninstallKeep || !strings.Contains(item.Reason, "something else 1.0") {
		t.Fatalf("not a kander binary: %+v", item)
	}

	f.version = "kander 2.0.0"
	uninstallPackaged = func() bool { return true }
	for path, item := range reasons() {
		if item.Action != UninstallSkip || item.Reason != config.Text("install.uninstall_binary_msix") {
			t.Fatalf("MSIX install: %s %+v", path, item)
		}
	}
	uninstallPackaged = func() bool { return false }

	if runtime.GOOS == "windows" {
		return
	}
	// A Homebrew entry is a link into a Cellar; it stays and the plan names brew uninstall.
	cellar := filepath.Join(f.home, "homebrew", "Cellar", "kander", "2.0.0", "bin", "kander")
	writeIntegrateFile(t, cellar, "binary")
	if err := os.Remove(f.entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellar, f.entry); err != nil {
		t.Fatal(err)
	}
	if item := reasons()[f.entry]; item.Action != UninstallSkip || item.Reason != config.Text("install.uninstall_binary_brew") {
		t.Fatalf("Homebrew link: %+v", item)
	}
	// Any other link is never followed.
	other := filepath.Join(f.home, "elsewhere", "kander")
	writeIntegrateFile(t, other, "binary")
	if err := os.Remove(f.entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, f.entry); err != nil {
		t.Fatal(err)
	}
	if item := reasons()[f.entry]; item.Action != UninstallKeep {
		t.Fatalf("other link: %+v", item)
	}
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), f.paths), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if readBytes(t, other) != "binary" || readBytes(t, cellar) != "binary" {
		t.Fatal("deleted through a link")
	}
}

func TestUninstallRenamesABusyBinaryAside(t *testing.T) {
	f := newUninstallFixture(t)
	savedBusy, savedAside, savedRemove, savedStart := fileIsBusy, asideOnBusy, removeEntry, startDeferredDelete
	t.Cleanup(func() {
		fileIsBusy, asideOnBusy, removeEntry, startDeferredDelete = savedBusy, savedAside, savedRemove, savedStart
	})
	busy := errors.New("in use")
	fileIsBusy = func(err error) bool { return errors.Is(err, busy) }
	asideOnBusy = func() bool { return true }
	removeEntry = func(root, path string) (bool, error) {
		if path == f.entry {
			return false, busy
		}
		return savedRemove(root, path)
	}
	var started []string
	startDeferredDelete = func(path string, pid int) error {
		if pid != os.Getpid() {
			t.Errorf("helper waits for pid %d", pid)
		}
		started = append(started, path)
		return nil
	}
	item := UninstallItem{Action: UninstallRemove, Path: f.entry}
	var out bytes.Buffer
	if err := removeBinary(item, &out); err != nil {
		t.Fatal(err)
	}
	aside := f.entry + ".old"
	if _, err := os.Lstat(f.entry); !os.IsNotExist(err) {
		t.Fatalf("the entry name stays: %v", err)
	}
	if readBytes(t, aside) != "binary" || len(started) != 1 || started[0] != aside {
		t.Fatalf("aside=%q started=%v", readBytes(t, aside), started)
	}
	if !strings.Contains(out.String(), config.Text("install.uninstall_binary_deferred", f.entry, aside)) {
		t.Fatalf("output=%s", out.String())
	}

	// Without the helper the leftover path is reported.
	writeIntegrateFile(t, f.entry, "binary")
	startDeferredDelete = func(string, int) error { return errHelper }
	out.Reset()
	if err := removeBinary(item, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), aside) || !strings.Contains(out.String(), errHelper.Error()) {
		t.Fatalf("leftover not reported: %s", out.String())
	}
}

var errHelper = errors.New("powershell.exe not found")

func TestEncodePowerShell(t *testing.T) {
	script := "Remove-Item -LiteralPath 'C:\\Users\\o''neil\\kander.exe.old'"
	data, err := base64.StdEncoding.DecodeString(encodePowerShell(script))
	if err != nil || len(data)%2 != 0 {
		t.Fatalf("data=%v err=%v", data, err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Fatalf("decoded %q", got)
	}
}
