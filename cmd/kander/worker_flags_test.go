package main

import (
	"testing"

	"github.com/dualface/kander/v2/internal/remote"
)

// The internal worker takes no flag at all, global ones included, wherever they stand.
func TestWorkerRefusesEveryFlag(t *testing.T) {
	// A worker that was not refused would start in a client home of its own.
	t.Setenv(remote.EnvClientHome, t.TempDir())
	for _, args := range [][]string{
		{"worker", "--lang", "cn"},
		{"--lang", "cn", "worker"},
		{"worker", "--json=false"},
		{"--json=false", "worker"},
		{"worker", "--json"},
	} {
		// The worker would start and wait if it ran, so the test only sees a refusal.
		result := runCLI(t, args...)
		if result.code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}
