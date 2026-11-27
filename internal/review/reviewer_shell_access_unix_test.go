//go:build unix

package review

import (
	"os"
	"strings"
	"testing"
)

// agy, grok, kimi, and opencode run read-only Git through their shell tools, so a
// new run with any of them reaches the reviewer instead of being refused.
func TestReviewLaunchesShellReviewers(t *testing.T) {
	h, _, task, requirements := remoteReviewHarness(t, nil)
	for _, agent := range []string{"agy", "grok", "kimi", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			t.Setenv(strings.ToUpper(agent)+"_REVIEW_BIN", h.fake)
			positional := []string{h.repo, h.base, h.head, "PMQA", "Inspect only the review range"}
			for _, bound := range []bool{false, true} {
				if err := os.Remove(h.argvLog); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				args := []string{agent}
				if bound {
					args = append(args, "--task", task.TaskID, "--run-id", "shell-"+agent,
						"--batch-id", "shell", "--requirements-file", requirements)
				}
				_, _, stderr := captureRun(t, append(args, positional...))
				if strings.Contains(stderr, "cannot run read-only Git") {
					t.Fatalf("bound=%t: reviewer refused: %s", bound, stderr)
				}
				if _, err := os.Stat(h.argvLog); err != nil {
					t.Fatalf("bound=%t: reviewer not launched: %v; stderr=%s", bound, err, stderr)
				}
			}
		})
	}
}
