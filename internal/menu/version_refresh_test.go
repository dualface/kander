package menu

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install/installtest"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/version"
)

// refreshEnv isolates a version refresh: a temporary HOME, client home, and scope config, the
// running version set to 2.0.0, and the running binary taken as the global entry.
type refreshEnv struct {
	home, client string
	paths        config.InstallPaths
}

func newRefreshEnv(t *testing.T) refreshEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	client := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, client)
	t.Setenv(config.EnvConfig, filepath.Join(home, ".config", "kander", "config.json"))
	t.Chdir(t.TempDir())
	oldVersion := version.Version
	version.Version = "2.0.0"
	versionRefreshInjected.Store(true)
	oldGlobal := runningFromGlobalEntry
	runningFromGlobalEntry = func() (bool, error) { return true, nil }
	oldAsk := askDoctorAgentChoice
	askDoctorAgentChoice = func(string, []choice, string) (string, error) {
		t.Fatal("the version refresh asked a question")
		return "", nil
	}
	t.Cleanup(func() {
		version.Version = oldVersion
		versionRefreshInjected.Store(false)
		runningFromGlobalEntry = oldGlobal
		askDoctorAgentChoice = oldAsk
	})
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	return refreshEnv{home: home, client: client, paths: paths}
}

func (e refreshEnv) recorded(t *testing.T) string {
	t.Helper()
	record, err := remote.ReadHostRecord(e.client)
	if err != nil {
		t.Fatal(err)
	}
	return record.Version
}

func (e refreshEnv) entry() string {
	return filepath.Join(e.paths.RulesDir, "KANDER-AGENTS.md")
}

func refreshNoticeCodes(r VersionRefresh) []string {
	var codes []string
	for _, notice := range r.Notices() {
		codes = append(codes, notice.Code)
	}
	return codes
}

// A version host.json does not record refreshes once: the rules, the leader skill, the version,
// and the removal of the startup file earlier releases wrote. The next call finds it current.
func TestVersionRefreshRunsOnceAndRecordsTheVersion(t *testing.T) {
	env := newRefreshEnv(t)
	if err := os.MkdirAll(filepath.Join(env.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.paths.RulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(env.paths.RulesDir, "kander-startup-state.json")
	if err := os.WriteFile(startup, []byte(`{"version":"1.9.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(env.paths.RulesDir, "notes.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := RefreshForVersion()
	if got.Status != VersionRefreshDone || !got.Ran() || len(got.Failures) > 0 {
		t.Fatalf("refresh = %+v", got)
	}
	if codes := refreshNoticeCodes(got); len(codes) != 1 || codes[0] != NoticeVersionRefreshed {
		t.Fatalf("notices = %v", codes)
	}
	if env.recorded(t) != "2.0.0" {
		t.Fatalf("recorded %q", env.recorded(t))
	}
	if _, err := os.Stat(env.entry()); err != nil {
		t.Fatalf("rules entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".claude", "skills", "kander-leader", "SKILL.md")); err != nil {
		t.Fatalf("leader skill: %v", err)
	}
	if _, err := os.Lstat(startup); !os.IsNotExist(err) {
		t.Fatalf("the startup file stays: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("another file of the rules directory was removed: %v", err)
	}

	if err := os.Remove(env.entry()); err != nil {
		t.Fatal(err)
	}
	if again := RefreshForVersion(); again.Status != VersionRefreshCurrent || again.Ran() || len(again.Notices()) != 0 {
		t.Fatalf("second refresh = %+v", again)
	}
	if _, err := os.Stat(env.entry()); !os.IsNotExist(err) {
		t.Fatal("a current version refreshed again")
	}
}

// A failed step records nothing and reports the failure; the next call tries again.
func TestVersionRefreshFailureRecordsNothingAndRetries(t *testing.T) {
	env := newRefreshEnv(t)
	if err := os.MkdirAll(filepath.Dir(env.paths.RulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.paths.RulesDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RefreshForVersion()
	if got.Status != VersionRefreshFailed || !got.Ran() || len(got.Failures) == 0 {
		t.Fatalf("refresh = %+v", got)
	}
	if codes := refreshNoticeCodes(got); len(codes) != 1 || codes[0] != NoticeVersionRefreshFailed {
		t.Fatalf("notices = %v", codes)
	}
	if text := got.Notices()[0].Text; strings.Contains(text, "\n") {
		t.Fatalf("the summary is not one line: %q", text)
	}
	if env.recorded(t) != "" {
		t.Fatalf("a failed refresh recorded %q", env.recorded(t))
	}

	if err := os.Remove(env.paths.RulesDir); err != nil {
		t.Fatal(err)
	}
	if again := RefreshForVersion(); again.Status != VersionRefreshDone {
		t.Fatalf("retry = %+v", again)
	}
	if env.recorded(t) != "2.0.0" {
		t.Fatalf("recorded %q after the retry", env.recorded(t))
	}
}

// A rule file changed locally is kept and listed as pending; pending items do not fail the
// refresh, which records the version and asks nothing.
func TestVersionRefreshPendingItemsAreNotFailures(t *testing.T) {
	env := newRefreshEnv(t)
	if got := RefreshForVersion(); got.Status != VersionRefreshDone {
		t.Fatalf("first refresh = %+v", got)
	}
	if err := os.WriteFile(env.entry(), []byte("my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	version.Version = "2.1.0"
	got := RefreshForVersion()
	if got.Status != VersionRefreshDone || len(got.Failures) > 0 || len(got.Pending) != 1 {
		t.Fatalf("refresh = %+v", got)
	}
	if want := config.Text("menu.doctor_confirm_modified_rules", 1); got.Pending[0] != want {
		t.Fatalf("pending = %q, want %q", got.Pending[0], want)
	}
	if text := got.Notices()[0].Text; !strings.Contains(text, got.Pending[0]) {
		t.Fatalf("the summary leaves the pending item out: %q", text)
	}
	if data, _ := os.ReadFile(env.entry()); string(data) != "my own rules\n" {
		t.Fatalf("the modified rule file was replaced: %q", data)
	}
	if env.recorded(t) != "2.1.0" {
		t.Fatalf("recorded %q", env.recorded(t))
	}
}

// The refresh writes no file of the Git checkout it runs in, even one bound to a project with an
// overlay and a registration on this host.
func TestVersionRefreshWritesNoGitRepository(t *testing.T) {
	env := newRefreshEnv(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command(git, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	overlay := `{"project":{"name":"app"}}`
	if err := os.WriteFile(filepath.Join(repo, config.OverlayFilename), []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# App\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "init"}} {
		if out, err := exec.Command(git, append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := remote.RegisterProject(env.client, "http://hosts:7878", "app", repo, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := remote.StoreHostScan(env.client, remote.HostScan{Path: os.Getenv("PATH"), Projects: []remote.ProjectScan{}}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	if got := RefreshForVersion(); got.Status != VersionRefreshDone || len(got.Pending) > 0 {
		t.Fatalf("refresh = %+v", got)
	}
	out, err := exec.Command(git, "-C", repo, "status", "--porcelain", "--ignored").CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("the refresh changed the checkout: %v\n%s", err, out)
	}
}

// The scan of the refresh resolves the agents with the PATH and the config_home.env values
// host.json recorded, not with this process's, and keeps both.
func TestVersionRefreshScansWithTheRecordedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a POSIX script")
	}
	env := newRefreshEnv(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	recorded := t.TempDir()
	if err := os.WriteFile(filepath.Join(recorded, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	recordedPath := recorded + string(os.PathListSeparator) + filepath.Dir(git)
	t.Setenv("PATH", filepath.Dir(git))
	t.Setenv("CLAUDE_CONFIG_DIR", "/process/claude")
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command(git, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if _, err := remote.RegisterProject(env.client, "http://hosts:7878", "app", repo, time.Now()); err != nil {
		t.Fatal(err)
	}
	old := remote.HostScan{ScannedAt: time.Now().Add(-time.Hour), Path: recordedPath, Projects: []remote.ProjectScan{{
		ServerURL: "http://hosts:7878", Project: "app",
		Agents:    []remote.AgentScan{{Name: "claude", Path: "/gone/claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/recorded/claude"}}},
		Launchers: []string{},
	}}}
	if err := remote.StoreHostScan(env.client, old); err != nil {
		t.Fatal(err)
	}

	if got := RefreshForVersion(); got.Status != VersionRefreshDone || len(got.Pending) > 0 {
		t.Fatalf("refresh = %+v", got)
	}
	record, err := remote.ReadHostRecord(env.client)
	if err != nil {
		t.Fatal(err)
	}
	if record.Scan.Path != recordedPath || !record.Scan.ScannedAt.After(old.ScannedAt) {
		t.Fatalf("scan path %q at %v", record.Scan.Path, record.Scan.ScannedAt)
	}
	scan, _ := record.ScanOf("http://hosts:7878", "app")
	index := slices.IndexFunc(scan.Agents, func(a remote.AgentScan) bool { return a.Name == "claude" })
	if index < 0 {
		t.Fatalf("claude is missing: %+v", scan.Agents)
	}
	agent := scan.Agents[index]
	if agent.Path != filepath.Join(recorded, "claude") || agent.Env["CLAUDE_CONFIG_DIR"] != "/recorded/claude" {
		t.Fatalf("claude = %+v", agent)
	}
}

// A registered project without a recorded PATH skips the scan and names kander doctor as pending;
// the refresh still completes.
func TestVersionRefreshWithoutRecordedPathSkipsTheScan(t *testing.T) {
	env := newRefreshEnv(t)
	if _, err := remote.RegisterProject(env.client, "http://hosts:7878", "app", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got := RefreshForVersion()
	if got.Status != VersionRefreshDone || !slices.Contains(got.Pending, config.Text("versionrefresh.scan_needs_doctor")) {
		t.Fatalf("refresh = %+v", got)
	}
	record, err := remote.ReadHostRecord(env.client)
	if err != nil || record.Scan != nil {
		t.Fatalf("scan = %+v %v", record.Scan, err)
	}
}

// A binary that is not the global entry only names the mismatch: nothing is written.
func TestVersionRefreshOutsideTheGlobalEntryOnlyReports(t *testing.T) {
	env := newRefreshEnv(t)
	runningFromGlobalEntry = func() (bool, error) { return false, nil }
	got := RefreshForVersion()
	if got.Status != VersionRefreshNotGlobalEntry || got.Ran() {
		t.Fatalf("refresh = %+v", got)
	}
	notices := got.Notices()
	if len(notices) != 1 || notices[0].Code != NoticeVersionRefreshSkipped || strings.Contains(notices[0].Text, "\n") {
		t.Fatalf("notices = %+v", notices)
	}
	for _, path := range []string{env.paths.RulesDir, env.client} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was written: %v", path, err)
		}
	}
	runningFromGlobalEntry = func() (bool, error) { return false, errors.New("unreadable") }
	if got := RefreshForVersion(); got.Status != VersionRefreshFailed || got.Ran() {
		t.Fatalf("refresh with an unreadable entry = %+v", got)
	}
}

// A process that finds the lock taken neither waits nor refreshes; after the lock is free, one
// that finds the version already recorded under the lock does not refresh either.
func TestVersionRefreshSkipsWhenTheLockIsTaken(t *testing.T) {
	env := newRefreshEnv(t)
	release, taken, err := remote.TryVersionRefreshLock(env.client)
	if err != nil || !taken {
		t.Fatalf("lock: %v %v", taken, err)
	}
	done := make(chan VersionRefresh, 1)
	go func() { done <- RefreshForVersion() }()
	select {
	case got := <-done:
		if got.Status != VersionRefreshBusy || len(got.Notices()) != 0 {
			t.Fatalf("refresh = %+v", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the refresh waited for the lock")
	}
	release()
	if _, err := os.Lstat(env.paths.RulesDir); !os.IsNotExist(err) {
		t.Fatalf("a refresh without the lock wrote: %v", err)
	}
	if env.recorded(t) != "" {
		t.Fatal("a refresh without the lock recorded the version")
	}

	// The global entry check runs before the lock: a process that refreshed in between is seen
	// by the second read.
	runningFromGlobalEntry = func() (bool, error) {
		if err := remote.RecordHostVersion(env.client, "2.0.0"); err != nil {
			t.Fatal(err)
		}
		return true, nil
	}
	if got := RefreshForVersion(); got.Status != VersionRefreshCurrent {
		t.Fatalf("refresh after another process = %+v", got)
	}
	if _, err := os.Lstat(env.paths.RulesDir); !os.IsNotExist(err) {
		t.Fatal("the second read under the lock did not stop the refresh")
	}
}

// The refresh never reads the terminal and prints nothing itself: with a modified rule file doctor
// would ask about, it returns with the file pending, and a stdin that never answers does not hold
// it.
func TestVersionRefreshAsksNothing(t *testing.T) {
	env := newRefreshEnv(t)
	if got := RefreshForVersion(); got.Status != VersionRefreshDone {
		t.Fatalf("first refresh = %+v", got)
	}
	if err := os.WriteFile(env.entry(), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	version.Version = "2.1.0"
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinW.Close()
	defer stdinR.Close()
	oldStdin := os.Stdin
	os.Stdin = stdinR
	defer func() { os.Stdin = oldStdin }()

	var got VersionRefresh
	finished := make(chan struct{})
	stdout, stderr := captureProcessOutput(t, func() {
		go func() { got = RefreshForVersion(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(60 * time.Second):
			t.Fatal("the refresh waited for input")
		}
	})
	if stdout != "" || stderr != "" {
		t.Fatalf("the refresh printed: stdout=%q stderr=%q", stdout, stderr)
	}
	if got.Status != VersionRefreshDone || len(got.Pending) == 0 {
		t.Fatalf("refresh = %+v", got)
	}
}

// captureProcessOutput runs f with os.Stdout and os.Stderr connected to pipes and returns what
// was written to them.
func captureProcessOutput(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	read := func(r *os.File, into *string, done chan<- struct{}) {
		data, _ := io.ReadAll(r)
		*into = string(data)
		close(done)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go read(outR, &stdout, outDone)
	go read(errR, &stderr, errDone)
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = outW.Close()
		_ = errW.Close()
		<-outDone
		<-errDone
		_ = outR.Close()
		_ = errR.Close()
	}()
	f()
	return stdout, stderr
}

// The refresh cleans the global injection of the current configuration directories even when the
// marker already records them, reports it, and keeps the marker complete.
func TestVersionRefreshCleansTheGlobalInjectionWhateverTheMarker(t *testing.T) {
	env := newRefreshEnv(t)
	claudeDir := filepath.Join(env.home, ".claude")
	if err := os.MkdirAll(env.paths.RulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(env.paths.RulesDir, "kander-global-cleanup.json")
	if err := os.WriteFile(marker, []byte(`{"config_dirs":["`+filepath.ToSlash(resolved)+`"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RefreshForVersion()
	if got.Status != VersionRefreshDone {
		t.Fatalf("refresh = %+v", got)
	}
	if data, _ := os.ReadFile(claude); string(data) != "# Mine\n" {
		t.Fatalf("CLAUDE.md = %q", data)
	}
	if codes := refreshNoticeCodes(got); !slices.Equal(codes, []string{NoticeVersionRefreshed, "global_rules_cleaned"}) {
		t.Fatalf("notices = %v", codes)
	}
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), filepath.ToSlash(resolved)) {
		t.Fatalf("marker = %s %v", data, err)
	}
}
