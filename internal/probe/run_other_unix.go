//go:build unix && !darwin

package probe

import "errors"

var errUnsupported = errors.New("process group membership is not inspected on this platform")

// osGroupOnlyZombies never classifies an EPERM away outside macOS: Linux does
// not refuse to signal a group of zombies, so an EPERM there is a real refusal.
func osGroupOnlyZombies(pgid int) (bool, error) { return false, errUnsupported }
