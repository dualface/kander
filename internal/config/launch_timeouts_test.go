package config

import (
	"strings"
	"testing"
	"time"
)

func TestLaunchTimeoutOnlyRaises(t *testing.T) {
	for _, tc := range []struct {
		extended bool
		base     time.Duration
		want     time.Duration
	}{
		{false, 15 * time.Second, 15 * time.Second},
		{true, 15 * time.Second, ExtendedLaunchTimeout},
		{true, 150 * time.Second, 150 * time.Second},
	} {
		if got := LaunchTimeout(tc.extended, tc.base); got != tc.want {
			t.Fatalf("LaunchTimeout(%v, %v)=%v, want %v", tc.extended, tc.base, got, tc.want)
		}
	}
}

func TestExtendedLaunchTimeoutsSchema(t *testing.T) {
	raw, err := DocumentFromConfig(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	delete(raw, "extended_launch_timeouts")
	cfg, err := Validate(raw)
	if err != nil || cfg.ExtendedLaunchTimeouts {
		t.Fatalf("missing key: extended=%v err=%v", cfg != nil && cfg.ExtendedLaunchTimeouts, err)
	}
	raw["extended_launch_timeouts"] = "yes"
	if _, err := Validate(raw); err == nil || !strings.Contains(err.Error(), "extended_launch_timeouts") {
		t.Fatalf("non-boolean accepted: %v", err)
	}
	raw["extended_launch_timeouts"] = true
	if cfg, err = Validate(raw); err != nil || !cfg.ExtendedLaunchTimeouts {
		t.Fatalf("true: extended=%v err=%v", cfg != nil && cfg.ExtendedLaunchTimeouts, err)
	}
}

func TestExtendedLaunchTimeoutsOverlayAndFormat(t *testing.T) {
	setupHome(t)
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	merged, err := MergeScopeAndOverlay(scope, map[string]any{"extended_launch_timeouts": true})
	if err != nil || !merged.ExtendedLaunchTimeouts {
		t.Fatalf("overlay: extended=%v err=%v", merged != nil && merged.ExtendedLaunchTimeouts, err)
	}
	lines, err := FormatConfigLines(merged)
	if err != nil {
		t.Fatal(err)
	}
	want := Text("config.extended_launch_timeouts") + ": " + Text("rules.on")
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("format missing %q:\n%s", want, joined)
	}
}
