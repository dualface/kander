package launch

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func useEffectiveConfig(t *testing.T, cfg *config.Config, err error) {
	t.Helper()
	previous := loadEffective
	loadEffective = func() (*config.Config, error) { return cfg, err }
	t.Cleanup(func() { loadEffective = previous })
}

// Adoption can write a missing session marker when the card names the session
// or when the agent's sessions can be found again, which only the Codex
// rollout provider offers.
func TestMarkerRepairableByProviderCapability(t *testing.T) {
	useEffectiveConfig(t, config.DefaultConfig(), nil)
	for _, test := range []struct {
		session string
		want    bool
	}{
		{"codex", true},
		{"claude", false},
		{"devin", false},
		{"droid", false},
		{"cursor", false},
		{"opencode", false},
		{"claude 7b2c2e93-6f7e-4e5b-8c50-1f1f4f2a9d11", true},
		{"ghost", false},
		{"", false},
		{"codex bad id", false},
	} {
		if got := markerRepairable(test.session); got != test.want {
			t.Errorf("markerRepairable(%q)=%v, want %v", test.session, got, test.want)
		}
	}
}

func TestMarkerRepairableRefusesAnUnusableAgentAndAnUnreadableConfiguration(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UnusableAgents = map[string]string{"codex": "session.mode is unknown"}
	useEffectiveConfig(t, cfg, nil)
	if markerRepairable("codex") {
		t.Error("an agent the configuration marks unusable is repairable")
	}
	useEffectiveConfig(t, nil, errors.New("unreadable"))
	if markerRepairable("codex") {
		t.Error("an unreadable configuration is repairable")
	}
}

func TestSessionProviderLookupRefusesAnUnusableBuiltInAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	if _, err := sessionProviderOf(cfg, "codex"); err != nil {
		t.Fatalf("usable agent: %v", err)
	}
	cfg.UnusableAgents = map[string]string{"codex": "session.mode is unknown"}
	_, err := sessionProviderOf(cfg, "codex")
	if want := config.UnavailableAgentError(cfg, "codex").Error(); err == nil || err.Error() != want {
		t.Fatalf("err=%v, want %q", err, want)
	}
}

// Only an allocated session has an allocator to resolve; every other provider
// passes the check, and a missing allocator is the same plain refusal as before.
func TestRequireSessionAllocatorByProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	for _, agent := range []string{"claude", "cursor", "devin", "codex"} {
		provider, err := sessionProviderOf(cfg, agent)
		if err != nil {
			t.Fatal(err)
		}
		if err := requireSessionAllocator(provider, sessionLaunch{Definition: config.AgentFor(cfg, agent).Session}); err != nil {
			t.Errorf("%s: %v", agent, err)
		}
	}
	missing := filepath.Join(t.TempDir(), "allocator")
	definition := &config.AgentSessionDefinition{Mode: "allocated", Allocate: []string{missing}}
	provider, err := sessionProviderFor(nil, "custom", definition)
	if err != nil {
		t.Fatal(err)
	}
	err = requireSessionAllocator(provider, sessionLaunch{Definition: definition})
	if want := launchError("launch.agent_is_not_in_path", missing).Error(); err == nil || err.Error() != want {
		t.Fatalf("err=%v, want %q", err, want)
	}
}
