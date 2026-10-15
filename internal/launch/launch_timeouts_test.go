package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// startPaneTimeout starts a pane-delivery card whose agent never becomes ready
// and returns the fake-clock time the ready wait took plus every herdr
// wait-output argument the launch sent.
func startPaneTimeout(t *testing.T, extended bool) (time.Duration, string) {
	t.Helper()
	root, _, bin := setupBoard(t)
	cfg := usePaneAgent(t, root, bin, "herdr")
	cfg.ExtendedLaunchTimeouts = extended
	def := cfg.Agents["panecli"]
	def.PromptDelivery.Ready.TimeoutMS = 30000
	cfg.Agents["panecli"] = def
	log := filepath.Join(root, "herdr-extended.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_AGENT_OUTPUT", "still starting")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	freezeClock(t)
	id, _ := makeTodo(t, root, "pane-extended")
	start := nowFn()
	_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
	if err == nil || !strings.Contains(err.Error(), "still starting") {
		t.Fatalf("err=%v", err)
	}
	waits, readErr := os.ReadFile(log + ".wait-all")
	if readErr != nil {
		t.Fatal(readErr)
	}
	return nowFn().Sub(start), string(waits)
}

func TestExtendedLaunchTimeoutsRaisePaneWaits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	elapsed, waits := startPaneTimeout(t, true)
	if !strings.Contains(waits, "visible\n--timeout\n120000\n") {
		t.Fatalf("pane ready wait did not use 120000ms:\n%s", waits)
	}
	if elapsed < config.ExtendedLaunchTimeout || elapsed >= config.ExtendedLaunchTimeout+time.Second {
		t.Fatalf("prompt delivery waited %v, want %v", elapsed, config.ExtendedLaunchTimeout)
	}
}

func TestLaunchTimeoutsKeepBaseWhenOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	elapsed, waits := startPaneTimeout(t, false)
	if !strings.Contains(waits, "visible\n--timeout\n15000\n") {
		t.Fatalf("pane ready wait did not use 15000ms:\n%s", waits)
	}
	if elapsed < 30*time.Second || elapsed >= 31*time.Second {
		t.Fatalf("prompt delivery waited %v, want 30s", elapsed)
	}
}

// enumerationBudget records the context budget of one session enumeration.
func enumerationBudget(t *testing.T) *time.Duration {
	t.Helper()
	var budget time.Duration
	previous := enumerateSessionsFn
	enumerateSessionsFn = func(ctx context.Context, _ *config.SessionDiscovery, _ *process.AgentProgram, _, _ string) ([]string, error) {
		if deadline, ok := ctx.Deadline(); ok {
			budget = time.Until(deadline).Round(time.Second)
		}
		return []string{"existing"}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previous })
	return &budget
}

func TestExtendedLaunchTimeoutsRaiseSessionDiscovery(t *testing.T) {
	session := &config.AgentSessionDefinition{Mode: "discovered", Discovery: devinDiscovery(t)}
	for _, tc := range []struct {
		name     string
		extended bool
		want     time.Duration
	}{
		{"off", false, 30 * time.Second},
		{"on", true, config.ExtendedLaunchTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := LaunchPlan{extendedTimeouts: tc.extended}
			budget := enumerationBudget(t)
			if _, err := sessionDiscoverSnapshot(session, "task", true, &process.AgentProgram{}, t.TempDir(), plan.launchTimeout); err != nil {
				t.Fatal(err)
			}
			if *budget != tc.want {
				t.Fatalf("snapshot budget=%v, want %v", *budget, tc.want)
			}
			freezeClock(t)
			start := nowFn()
			if _, err := runSessionDiscoverHook(session, "task", map[string]struct{}{"existing": {}}, start, &process.AgentProgram{}, t.TempDir(), plan.launchTimeout); err == nil {
				t.Fatal("discovery without a new session succeeded")
			}
			if elapsed := nowFn().Sub(start); elapsed < tc.want || elapsed >= tc.want+time.Second {
				t.Fatalf("discovery waited %v, want %v", elapsed, tc.want)
			}
		})
	}
}

func TestExtendedLaunchTimeoutsRaiseCodexDiscovery(t *testing.T) {
	_, cwd := codexScanFixture(t)
	session := &config.AgentSessionDefinition{Mode: "hook:codex-rollout"}
	for _, tc := range []struct {
		name     string
		extended bool
		want     time.Duration
	}{
		{"off", false, sessionDiscoverWait},
		{"on", true, config.ExtendedLaunchTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := LaunchPlan{extendedTimeouts: tc.extended}
			freezeClock(t)
			start := nowFn()
			if _, err := runSessionDiscoverHook(session, "task", nil, start, nil, cwd, plan.launchTimeout); err == nil {
				t.Fatal("discovery without a rollout succeeded")
			}
			if elapsed := nowFn().Sub(start); elapsed < tc.want || elapsed >= tc.want+time.Second {
				t.Fatalf("codex discovery waited %v, want %v", elapsed, tc.want)
			}
		})
	}
}
