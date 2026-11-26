package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
)

func launchCoordinatorFixture(t *testing.T) (string, string, board.DispatchInput, board.CoordinatorCheckpoint) {
	t.Helper()
	root, task, _, in := launchWrapFixture(t)
	s, e := board.ReadSnapshot(root, task)
	if e != nil {
		t.Fatal(e)
	}
	// Test-only group assignment predates the checkpoint; normal frozen contracts
	// use the decision producer, never this fixture transaction.
	text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: 20260908-launch-coordinator-group", 1)
	if e = board.WithTransaction(root, board.LockScope{Tasks: []string{task}}, func(tx *board.Transaction) error { return tx.Put(task, "spec.md", text) }); e != nil {
		t.Fatal(e)
	}
	c, e := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: "20260908-launch-coordinator-group", Owner: "coordinator", Token: "one-session", Basis: "测试授权", Members: []string{task}})
	if e != nil {
		t.Fatal(e)
	}
	return root, task, in, c
}

func captureStdout(t *testing.T, run func() int) (int, []byte) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()
	status := run()
	w.Close()
	var out bytes.Buffer
	if _, e = io.Copy(&out, r); e != nil {
		t.Fatal(e)
	}
	return status, out.Bytes()
}

func TestReconcileOutcomeReachesExitCodeAndJSON(t *testing.T) {
	blocked := board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {Blocked: &board.CoordinatorBlock{Reason: "x"}}, "b": {}}}
	for _, tc := range []struct {
		name   string
		c      board.CoordinatorCheckpoint
		err    error
		status int
		result string
	}{
		{"complete", board.CoordinatorCheckpoint{Members: map[string]board.CoordinatorMember{"a": {}}}, nil, 0, "complete"},
		{"partial", blocked, nil, 3, "partial"},
		{"failed", blocked, &board.CoordinatorBlockedError{Group: "g", Reason: "x"}, 1, "failed"},
		{"refused", board.CoordinatorCheckpoint{}, errors.New("checkpoint CAS"), 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, out := captureStdout(t, func() int { return finishReconcile(tc.c, tc.err) })
			if status != tc.status {
				t.Fatalf("status %d", status)
			}
			if tc.result == "" {
				if len(out) != 0 {
					t.Fatalf("a refused request printed %q", out)
				}
				return
			}
			var decoded struct {
				Result string `json:"result"`
			}
			if e := json.Unmarshal(out, &decoded); e != nil || decoded.Result != tc.result {
				t.Fatalf("%s %v", out, e)
			}
		})
	}
}

func TestCoordinatorCommandRejectsInvalidInputBeforeClaim(t *testing.T) {
	root, _, _, c := launchCoordinatorFixture(t)
	p := filepath.Join(t.TempDir(), "request.json")
	if e := os.WriteFile(p, []byte(`{"group_id":"x","group_id":"y"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if RunCoordinatorRemote([]string{"claim", p}) == 0 {
		t.Fatal("ambiguous JSON accepted")
	}
	after, e := board.ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(c, after) {
		t.Fatal("invalid command changed checkpoint", e)
	}
}
