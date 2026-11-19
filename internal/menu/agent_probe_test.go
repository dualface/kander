//go:build unix

package menu

import (
	"github.com/dualface/kander/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDoctorAndPanelProbeAgentOverride(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.fakeBin, "renamed-agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho renamed-version\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", h.fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Path: path}, "helper": {Path: path, Dialect: "claude"}}
	states := findAgents(cfg)
	t.Setenv("PATH", h.fakeBin)
	states = findAgents(cfg)
	if reviewerUsable(states["codex"]) {
		t.Fatal("execution wrapper offered as reviewer")
	}
	if states["helper"].Review {
		t.Fatal("dialect wrapper offered as reviewer")
	}
	if states["codex"].Path != path || states["helper"].Version != "renamed-version" {
		t.Fatal(states)
	}
	t.Setenv(config.EnvConfig, h.configPath)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, out, diagnostic := h.run("doctor")
	out += diagnostic
	if !strings.Contains(out, "Codex: renamed-version") || !strings.Contains(out, "helper: renamed-version") {
		t.Fatalf("%s %s", out, diagnostic)
	}
}

// Each fake agent announces itself and then waits until all three have
// started, so only probes that overlap in time can report the version. The
// judgment never reads the clock: a loaded machine delays the probes but cannot
// break the overlap. The barrier gives up after 300 polls, below the 5 second
// probe timeout, so a sequential findAgents fails with a readable value instead
// of a timeout. PATH holds only the fake directory, so the real agent CLIs of
// the machine are not probed, and the scripts call only shell builtins and
// /bin/sleep because they run with that PATH.
func TestFindAgentsProbesConcurrently(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	started := filepath.Join(h.root, "probe-started")
	if err := os.MkdirAll(started, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	cfg.Agents = map[string]config.AgentDefinition{}
	names := []string{"slow-a", "slow-b", "slow-c"}
	for _, name := range names {
		script := strings.NewReplacer("@DIR@", started, "@NAME@", name, "@COUNT@", strconv.Itoa(len(names))).Replace(`#!/bin/sh
dir='@DIR@'
: > "$dir/@NAME@"
i=0
while [ "$i" -lt 300 ]; do
  n=0
  for f in "$dir"/*; do
    [ -e "$f" ] && n=$((n+1))
  done
  if [ "$n" -ge @COUNT@ ]; then
    echo slow-version
    exit 0
  fi
  /bin/sleep 0.01
  i=$((i+1))
done
echo barrier-timeout
`)
		path := filepath.Join(h.fakeBin, name)
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		cfg.Agents[name] = config.AgentDefinition{Path: path, Dialect: "claude"}
	}
	states := findAgents(cfg)
	for _, name := range names {
		if states[name].Version != "slow-version" {
			t.Fatalf("%s did not run alongside the other probes: version %q", name, states[name].Version)
		}
	}
}

func TestPanelCanConfigureUnselectedRenamedBuiltin(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	t.Setenv(config.EnvConfig, h.configPath)
	wrapper := filepath.Join(h.fakeBin, "kander-codex")
	for _, name := range []string{"claude", "kander-codex"} {
		if err := os.WriteFile(filepath.Join(h.fakeBin, name), []byte("#!/bin/sh\necho fixture\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range s.ExecutionChoicesFor("claude") {
		if choice.Value == "codex" {
			found = true
		}
	}
	if !found {
		t.Fatal("unavailable Codex cannot be selected")
	}
	s.SetExecutionAgent("small", "codex")
	if _, err := config.Update(func(cfg *config.Config) error {
		if cfg.Agents == nil {
			cfg.Agents = map[string]config.AgentDefinition{}
		}
		cfg.Agents["codex"] = config.AgentDefinition{Path: wrapper}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(false)
	if err != nil || config.AgentPath(got, "codex") != wrapper {
		t.Fatalf("%+v %v", got, err)
	}
}
