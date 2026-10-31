package launch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
)

// blockedRecoverRequest returns a recover request for a blocked member and a tmux
// that reports its recorded pane gone and answers list-panes with paneRows.
func blockedRecoverRequest(t *testing.T, paneRows func(reference string) string) (string, string, board.CoordinatorRecover) {
	t.Helper()
	root, task, _, c := launchCoordinatorFixture(t)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	r := board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{task: {Revision: s.Revision, DispatchID: "stale-round", Epoch: 1}}}
	blocked, _ := ReconcileCoordinator(context.Background(), root, r)
	if blocked.Members[task].Blocked == nil {
		t.Fatal("no block")
	}
	t.Setenv("KANBAN_TMUX_LIST_PANES", paneRows(liveness.ParseTaskSession(s.Text).Reference))
	writeRecoverTmux(t, root, `if [ "$1" = "list-panes" ]; then
  printf '%s\n' "$KANBAN_TMUX_LIST_PANES"
  exit 0
fi
printf '%s\n' "can't find pane: $4" >&2
exit 1
`)
	return root, task, board.CoordinatorRecover{GroupID: blocked.GroupID, Member: task, ExpectedRevision: blocked.Revision, ExpectedEpoch: blocked.Authority.Epoch, Decision: "user-decision", Reason: "reset"}
}

func writeRecoverTmux(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "fake-bin", "tmux"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runRecover(t *testing.T, request board.CoordinatorRecover) (int, string) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recover.json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	code := 0
	_, errOut, _ := capture(t, func() error {
		code = RunCoordinator([]string{"recover", path})
		return nil
	})
	return code, errOut
}

func paneRow(pane, reference string) string {
	return pane + "\t$9\tmoved\t@9\tclaude\t0\t" + reference + "\t"
}

// The drifted executor is found by the real classification, so this fails when
// the probe stops searching a stale address.
func TestRecoverRefusesAnExecutorThatDrifted(t *testing.T) {
	root, task, request := blockedRecoverRequest(t, func(reference string) string { return paneRow("%9", reference) })
	before, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	code, errOut := runRecover(t, request)
	if code == 0 || !strings.Contains(errOut, liveness.Drifted) {
		t.Fatalf("code=%d %s", code, errOut)
	}
	after, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Entry.State != before.Entry.State {
		t.Fatalf("card changed: %+v", after.Entry)
	}
	c, err := board.ReadCoordinatorCheckpointContext(context.Background(), root, request.GroupID)
	if err != nil || len(c.Recoveries) != 0 {
		t.Fatalf("%+v %v", c.Recoveries, err)
	}
}

func TestRecoverResetsWhenTheLookupFindsNoExecutor(t *testing.T) {
	for name, rows := range map[string]func(string) string{
		"stopped": func(string) string { return paneRow("%9", "another-session") },
		"unknown": func(reference string) string { return paneRow("%8", reference) + "\n" + paneRow("%9", reference) },
	} {
		t.Run(name, func(t *testing.T) {
			root, _, request := blockedRecoverRequest(t, rows)
			if code, errOut := runRecover(t, request); code != 0 {
				t.Fatalf("code=%d %s", code, errOut)
			}
			c, err := board.ReadCoordinatorCheckpointContext(context.Background(), root, request.GroupID)
			if err != nil || len(c.Recoveries) != 1 || c.Recoveries[0].Mode != board.RecoverReset {
				t.Fatalf("%+v %v", c.Recoveries, err)
			}
		})
	}
}

// A hung terminal must leave the board write its share of the command deadline.
func TestRecoverProbeStopsAtItsBudget(t *testing.T) {
	root, _, request := blockedRecoverRequest(t, func(string) string { return "" })
	writeRecoverTmux(t, root, "exec sleep 30\n")
	saved := recoverProbeBudget
	t.Cleanup(func() { recoverProbeBudget = saved })
	recoverProbeBudget = 200 * time.Millisecond
	started := time.Now()
	if code, errOut := runRecover(t, request); code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("probe took %s", elapsed)
	}
}
