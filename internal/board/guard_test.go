package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardWriteRejectsMissingDirectChildAndReportsMove(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("guard-card")
	capture(t, func() int { return newCard("chore", "guard-card", "护栏测试", false) })
	existing := filepath.Join(root, "backlog", taskID, "spec.md")
	guard := func(path string) GuardVerdict {
		t.Helper()
		verdict, err := GuardWrite(root, path)
		if err != nil {
			t.Fatal(err)
		}
		return verdict
	}

	// Existing card: allowed.
	if verdict := guard(existing); !verdict.Allowed {
		t.Fatalf("existing card rejected: %s", verdict.Reason)
	}
	// Direct child of a state directory that does not exist while the same ID lives elsewhere: rejected with a moved-card hint.
	stale := filepath.Join(root, "working", taskID+".md")
	if verdict := guard(stale); verdict.Allowed || !strings.Contains(verdict.Reason, "backlog") {
		t.Fatalf("stale path: %+v", verdict)
	}
	// Direct child and the ID is nowhere on the board: also rejected (new cards only come from kander new).
	fresh := filepath.Join(root, "todo", "20990101-nonexistent-task.md")
	if verdict := guard(fresh); verdict.Allowed || !strings.Contains(verdict.Reason, "kander new") {
		t.Fatalf("fresh direct child: %+v", verdict)
	}
	// File inside a directory card: allowed when the card entry exists, rejected when it does not (otherwise the write would silently rebuild the whole directory card).
	dirID := todayID("guard-dir")
	if err := os.MkdirAll(filepath.Join(root, "review", dirID), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "review", dirID, "plan.md")
	if verdict := guard(inner); !verdict.Allowed {
		t.Fatalf("existing directory card inner file rejected: %s", verdict.Reason)
	}
	staleInner := filepath.Join(root, "working", dirID, "report.md")
	if verdict := guard(staleInner); verdict.Allowed || !strings.Contains(verdict.Reason, "review") {
		t.Fatalf("moved directory card inner file: %+v", verdict)
	}
	if verdict := guard(filepath.Join(root, "working", taskID, "plan.md")); verdict.Allowed {
		t.Fatal("inner file under missing card entry must be rejected")
	}
	// Outside the board and outside a state directory: allowed.
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if verdict := guard(outside); !verdict.Allowed {
		t.Fatal("outside path rejected")
	}
	if verdict := guard(filepath.Join(root, "notes.md")); !verdict.Allowed {
		t.Fatal("non-state kanban file rejected")
	}
}
