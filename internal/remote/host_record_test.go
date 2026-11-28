package remote

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

func TestHostRecordRegistersAndKeepsWhatItDoesNotKnow(t *testing.T) {
	home := t.TempDir()
	at := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	if previous, err := RegisterProject(home, "http://hosts:7878", "quicktui", "/w/one", at); err != nil || previous != "" {
		t.Fatalf("first registration: %q %v", previous, err)
	}
	if previous, err := RegisterProject(home, "http://hosts:7878", "quicktui", "/w/one", at); err != nil || previous != "" {
		t.Fatalf("same registration: %q %v", previous, err)
	}
	if previous, err := RegisterProject(home, "http://hosts:7878", "quicktui", "/w/two", at); err != nil || previous != "/w/one" {
		t.Fatalf("replacement: %q %v", previous, err)
	}
	// The key is server and project: another server keeps its own registration.
	if _, err := RegisterProject(home, "http://other:7878", "quicktui", "/w/three", at); err != nil {
		t.Fatal(err)
	}
	registered, added, err := RegisterProjectIfMissing(home, "http://hosts:7878", "quicktui", "/w/four", at)
	if err != nil || added || registered != "/w/two" {
		t.Fatalf("if missing kept=%q added=%v %v", registered, added, err)
	}

	// A newer writer's top-level key survives an update.
	path := filepath.Join(home, HostRecordName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["version"] = map[string]any{"binary": "2.1.0"}
	edited, _ := json.Marshal(raw)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StoreHostScan(home, HostScan{ScannedAt: at, Path: "/bin", Projects: []ProjectScan{{ServerURL: "http://hosts:7878", Project: "quicktui", Agents: []AgentScan{{Name: "claude", Path: "/bin/claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/c"}}}, Launchers: []string{"tmux-session"}}}}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"binary": "2.1.0"`) {
		t.Fatalf("unknown key lost:\n%s", data)
	}
	record, err := ReadHostRecord(home)
	if err != nil {
		t.Fatal(err)
	}
	if reg, ok := record.Registration("http://hosts:7878", "quicktui"); !ok || reg.Root != "/w/two" {
		t.Fatalf("registration = %+v %v", reg, ok)
	}
	if scan, ok := record.ScanOf("http://hosts:7878", "quicktui"); !ok || scan.Agents[0].Env["CLAUDE_CONFIG_DIR"] != "/c" {
		t.Fatalf("scan = %+v %v", scan, ok)
	}
	if urls := record.ServerURLs(); len(urls) != 2 {
		t.Fatalf("servers = %v", urls)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("host.json mode = %v %v", info.Mode(), err)
		}
	}

	// A record of another schema is not read as this one.
	if err := os.WriteFile(path, []byte(`{"schema":2,"projects":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadHostRecord(home); !errors.Is(err, ErrHostRecordSchema) {
		t.Fatalf("schema 2: %v", err)
	}
}

func TestHostNameDefaultsToTheClientIDHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	if err := os.WriteFile(filepath.Join(home, "client-id"), []byte("c-0123456789ab-buildbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if name, err := HostName(home); err != nil || name != "buildbox" {
		t.Fatalf("default name = %q %v", name, err)
	}
	cfg := config.DefaultConfig()
	cfg.Host.Name = "build-b"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if name, err := HostName(home); err != nil || name != "build-b" {
		t.Fatalf("configured name = %q %v", name, err)
	}
}

func TestInitRegistersTheMainWorktree(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, func(cfg *config.Config) { cfg.Server.URL, cfg.Project.Name = "", "" })
	repo := initRulesRepository(t)
	linked := filepath.Join(t.TempDir(), "linked")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "side", linked).CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v\n%s", err, out)
	}
	sub := filepath.Join(linked, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	code, out, errOut := runCommand(NewInitCommand, "--server", url, "--project", "golf")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	root := ProjectDir(sub)
	if !strings.Contains(out, config.Text("host.registration_written", root)) {
		t.Fatalf("registration not reported: %q", out)
	}
	home, _ := Home()
	record, err := ReadHostRecord(home)
	if err != nil {
		t.Fatal(err)
	}
	reg, ok := record.Registration(url, "golf")
	if !ok || reg.Root != root {
		t.Fatalf("registration = %+v %v, want root %s", reg, ok, root)
	}

	// A later init of another checkout replaces the registration and says so.
	other := initRulesRepository(t)
	code, out, errOut = runCommand(NewInitCommand, "--server", url, "--project", "golf", other)
	if code != 0 || !strings.Contains(out, config.Text("host.registration_replaced", ProjectDir(other), root)) {
		t.Fatalf("replacement: code=%d out=%q err=%q", code, out, errOut)
	}

	// The JSON result carries the registration.
	t.Chdir(other)
	code, out, _ = runCommand(NewInitCommand, "--json")
	var result struct {
		Registration struct {
			Root     string `json:"root"`
			Replaced string `json:"replaced"`
		} `json:"host_registration"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Registration.Root != ProjectDir(other) || result.Registration.Replaced != "" {
		t.Fatalf("json: code=%d out=%q", code, out)
	}
}

func TestInitOutsideGitRegistersNothing(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, func(cfg *config.Config) { cfg.Server.URL, cfg.Project.Name = "", "" })
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCommand(NewInitCommand, "--server", url, "--project", "golf")
	if code != 0 || !strings.Contains(errOut, config.Text("host.registration_not_git", dir)) {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	home, _ := Home()
	if _, err := os.Stat(filepath.Join(home, HostRecordName)); !os.IsNotExist(err) {
		t.Fatalf("host.json written outside Git: %v", err)
	}
}
