//go:build unix

package menu

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install/installtest"
	"github.com/dualface/kander/v2/internal/remote"
)

// A scan resolves the agents of each registered project with that checkout's
// configuration, records only their config_home.env values that are set, and
// lists only the launchers a process without a terminal can use.
func TestScanHostRecordsAgentsLaunchersAndPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	bin := t.TempDir()
	for _, name := range []string{"claude", "tmux"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	path := bin + string(os.PathListSeparator) + filepath.Dir(git)
	t.Setenv("PATH", path)
	t.Setenv("CLAUDE_CONFIG_DIR", "/accounts/two")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	clientHome, _ := remote.Home()
	if _, err := remote.RegisterProject(clientHome, "http://hosts:7878", "app", repo, time.Now()); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(t.TempDir(), "broken")
	if out, err := exec.Command("git", "init", "-q", broken).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(broken, config.OverlayFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.RegisterProject(clientHome, "http://hosts:7878", "gone", broken, time.Now()); err != nil {
		t.Fatal(err)
	}

	scan, problems, err := scanHost(clientHome, time.Now(), processScanEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if scan.Path != path || len(scan.Projects) != 2 {
		t.Fatalf("scan = %+v", scan)
	}
	if len(problems) != 1 || problems[0].Project != "gone" {
		t.Fatalf("problems = %+v", problems)
	}
	app := scan.Projects[0]
	claude := slices.IndexFunc(app.Agents, func(a remote.AgentScan) bool { return a.Name == "claude" })
	if claude < 0 {
		t.Fatalf("claude not scanned: %+v", app.Agents)
	}
	agent := app.Agents[claude]
	if agent.Path != filepath.Join(bin, "claude") || agent.Env["CLAUDE_CONFIG_DIR"] != "/accounts/two" || len(agent.Env) != 1 {
		t.Fatalf("claude = %+v", agent)
	}
	if slices.ContainsFunc(app.Agents, func(a remote.AgentScan) bool { return a.Name == "codex" }) {
		t.Fatalf("an agent without an executable was scanned: %+v", app.Agents)
	}
	if !slices.Contains(app.Launchers, "tmux-session") {
		t.Fatalf("launchers = %v", app.Launchers)
	}
	for _, name := range []string{"tmux", "foreground", "console", "dsh", "herdr", "herdr-core", "luvus"} {
		if slices.Contains(app.Launchers, name) {
			t.Fatalf("launcher %s needs a terminal but was recorded: %v", name, app.Launchers)
		}
	}
	record, err := remote.ReadHostRecord(clientHome)
	if err != nil || record.Scan == nil || record.Scan.Path != path {
		t.Fatalf("scan not stored: %+v %v", record.Scan, err)
	}
}

// A host without registrations writes no host.json.
func TestScanHostWithoutProjectsWritesNothing(t *testing.T) {
	clientHome := t.TempDir()
	scan, problems, err := scanHost(clientHome, time.Now(), processScanEnvironment())
	if err != nil || len(scan.Projects) != 0 || len(problems) != 0 {
		t.Fatalf("scan = %+v %v %v", scan, problems, err)
	}
	if _, err := os.Stat(filepath.Join(clientHome, remote.HostRecordName)); !os.IsNotExist(err) {
		t.Fatalf("host.json written: %v", err)
	}
}

// doctor registers a bound checkout that has no registration, and leaves a
// registration of another directory as it is.
func TestDoctorRegistersABoundCheckoutOnlyWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	repo := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, config.OverlayFilename), []byte(`{"server":{"url":"http://hosts:7878"},"project":{"name":"app"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	cfg, err := config.Load(true)
	if err != nil {
		t.Fatal(err)
	}
	clientHome, _ := remote.Home()
	root := remote.ProjectDir(repo)

	elsewhere := t.TempDir()
	if _, err := remote.RegisterProject(clientHome, "http://hosts:7878", "app", elsewhere, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !registerBoundCheckout(clientHome, cfg) {
		t.Fatal("a registration elsewhere is not a failure")
	}
	record, _ := remote.ReadHostRecord(clientHome)
	if reg, _ := record.Registration("http://hosts:7878", "app"); reg.Root != elsewhere {
		t.Fatalf("doctor replaced the registration: %+v", reg)
	}

	if err := os.Remove(filepath.Join(clientHome, remote.HostRecordName)); err != nil {
		t.Fatal(err)
	}
	if !registerBoundCheckout(clientHome, cfg) {
		t.Fatal("registration failed")
	}
	record, _ = remote.ReadHostRecord(clientHome)
	if reg, ok := record.Registration("http://hosts:7878", "app"); !ok || reg.Root != root {
		t.Fatalf("registration = %+v %v", reg, ok)
	}
}

// An Options save scans the registered projects again.
func TestOptionsSaveScansTheHostAgain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	clientHome, _ := remote.Home()
	if _, err := remote.RegisterProject(clientHome, "http://hosts:7878", "app", repo, time.Now()); err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	record, err := remote.ReadHostRecord(clientHome)
	if err != nil || record.Scan == nil || len(record.Scan.Projects) != 1 || record.Scan.Projects[0].Project != "app" {
		t.Fatalf("no scan after the save: %+v %v", record.Scan, err)
	}
}
