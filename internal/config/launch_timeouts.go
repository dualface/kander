package config

import "time"

// ExtendedLaunchTimeout is the floor extended_launch_timeouts applies to the
// launch-time waits: the launcher pane readiness, session discovery, and the
// pane prompt-delivery readiness.
const ExtendedLaunchTimeout = 120 * time.Second

// LaunchTimeout returns the budget of one launch-time wait. With extended on,
// a base below ExtendedLaunchTimeout is raised to it; a longer base is kept,
// so the switch never shortens a wait.
func LaunchTimeout(extended bool, base time.Duration) time.Duration {
	if extended && base < ExtendedLaunchTimeout {
		return ExtendedLaunchTimeout
	}
	return base
}

// validateExtendedLaunchTimeouts accepts a missing key as off, so configs
// written before the key existed keep the original waits.
func validateExtendedLaunchTimeouts(obj map[string]any) (bool, error) {
	raw, exists := obj["extended_launch_timeouts"]
	if !exists {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, configErrorf("config.extended_launch_timeouts_must_be_a_boolean")
	}
	return value, nil
}

// FormatSwitch is the localized on/off label of a boolean setting.
func FormatSwitch(on bool) string {
	if on {
		return Text("rules.on")
	}
	return Text("rules.off")
}
