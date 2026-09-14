//go:build unix

package review

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestOnlyCurrentReviewRoleAliasesRun(t *testing.T) {
	h, root, args := archiveHarness(t)
	for _, old := range []string{"PM", "pm", "CSA", "Hacker", "CodeSecurityAnalyst"} {
		code, _, stderr := h.review("codex", old, "goal")
		if code == 0 || !strings.Contains(stderr, "unsupported role") || !strings.Contains(stderr, "QA, Security") {
			t.Fatalf("%s: %d %s", old, code, stderr)
		}
	}
	args[len(args)-2] = "sEcUrItY"
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	if code, _, stderr := captureRun(t, args); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil || run.Role != "Security" {
		t.Fatalf("%+v %v", run, err)
	}
	if code, _, stderr := h.review("codex", "qA", "goal"); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
}
