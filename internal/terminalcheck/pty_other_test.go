//go:build !linux && !windows

package terminalcheck

import "testing"

func attachIsolatedClient(t *testing.T, _, _ string) bool {
	t.Helper()
	t.Log("focus: PTY attachment helper is unavailable on this platform; checking no-client skip")
	return false
}
