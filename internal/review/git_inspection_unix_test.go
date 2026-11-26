//go:build unix

package review

import (
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
)

func TestReviewRejectsNewRunsWithoutGitInspection(t *testing.T) {
	var runtimeWrites atomic.Int32
	h, _, task, requirements := remoteReviewHarness(t, func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/reviews/runtime/") {
			runtimeWrites.Add(1)
		}
		return false
	})
	for _, agent := range []string{"agy", "grok", "kimi", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			t.Setenv(strings.ToUpper(agent)+"_REVIEW_BIN", h.fake)
			positional := []string{h.repo, h.base, h.head, "PMQA", "Inspect only the review range"}
			for _, bound := range []bool{false, true} {
				args := []string{agent}
				if bound {
					args = append(args, "--task", task.TaskID, "--run-id", "no-git-"+agent,
						"--batch-id", "no-git", "--requirements-file", requirements)
				}
				code, _, stderr := captureRun(t, append(args, positional...))
				if code != 2 {
					t.Fatalf("bound=%t: got exit %d, want 2: %s", bound, code, stderr)
				}
				for _, want := range []string{
					agent + " cannot run read-only Git", "claude or codex", "REVIEW_<ROLE>_AGENT",
					"contract decision", "first-round review", "omit --previous-run-id", "new batch if needed",
				} {
					if !strings.Contains(stderr, want) {
						t.Errorf("bound=%t: diagnostic lacks %q: %s", bound, want, stderr)
					}
				}
			}
		})
	}
	if writes := runtimeWrites.Load(); writes != 0 {
		t.Errorf("rejected invocations made %d runtime writes", writes)
	}
	if _, err := os.Stat(h.argvLog); !os.IsNotExist(err) {
		t.Fatalf("reviewer launched: %v", err)
	}
}

func TestReviewRecoversExistingRunWithoutGitInspection(t *testing.T) {
	h, root, args := archiveHarnessFor(t, newCodexHarness(t), "agy")
	t.Setenv("AGY_REVIEW_BIN", h.fake)
	options, remaining, err := parseArchiveOptions(args)
	if err != nil {
		t.Fatal(err)
	}
	agent, rest, err := splitAgentArgs(remaining)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := validateContextMode(agent, rest, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// Seed a prepared run exactly as the old client did, without launching it.
	if fresh, err := archiveInvocation(&ctx, options, rest, root, board.FindingsSchemaReceiver); err != nil || !fresh {
		t.Fatalf("prepare old run: fresh=%t err=%v", fresh, err)
	}
	code, _, stderr := captureRun(t, args)
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if code != 2 || run.Reviewer != "agy" || run.Phase != "finalized" || run.ExecutionStatus != "interrupted" || !run.Published[run.TaskIDs[0]] {
		t.Fatalf("same-ID retry did not recover the old attempt: exit=%d run=%+v stderr=%s", code, run, stderr)
	}
	if _, err := os.Stat(h.argvLog); !os.IsNotExist(err) {
		t.Fatalf("recovery launched a reviewer: %v", err)
	}
}
