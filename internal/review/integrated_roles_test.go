//go:build unix

package review

import (
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestIntegratedReviewCLIAndAliases(t *testing.T) {
	for _, role := range []string{"PMQA", "Security"} {
		t.Run(role, func(t *testing.T) {
			_, root, args := archiveHarness(t)
			requirements := `{"PM":"N/A: integrated","QA":"N/A: integrated","CSA":"N/A: integrated","Hacker":"N/A: integrated","PMQA":"required","Security":"required"}`
			for i, arg := range args {
				if arg == "--requirements-file" {
					if err := os.WriteFile(args[i+1], []byte(requirements), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			args[len(args)-2] = strings.ToLower(role)
			t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
			if code, _, stderr := captureRun(t, args); code != 0 {
				t.Fatalf("%d %s", code, stderr)
			}
			run, err := board.ReadReviewRun(root, "stable")
			if err != nil || run.Role != role || run.ExecutionStatus != "ok" {
				t.Fatalf("%+v %v", run, err)
			}
			// Omitting the explicit reviewer exercises the other role-resolution path.
			args[len(args)-2] = strings.ToUpper(role)
			if code, _, stderr := captureRun(t, args[1:]); code != 0 {
				t.Fatalf("alias/retry %d %s", code, stderr)
			}
		})
	}
}
