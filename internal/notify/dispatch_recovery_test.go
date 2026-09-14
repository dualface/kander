package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func acceptedNotifyFixture(t *testing.T) (string, string, board.Dispatch) {
	t.Helper()
	root, task, d := durableNotifyFixture(t, time.Minute)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	d, err = board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed historical timestamps in the isolated fixture, never the real board.
	d.Input.CreatedAt = time.Now().UTC().Add(-12 * time.Minute)
	d.Input.ConfirmBy = d.Input.CreatedAt.Add(120 * time.Second)
	err = board.WithTransaction(root, board.LockScope{Tasks: []string{task}, Groups: []string{"00000000-dispatch-group"}}, func(tx *board.Transaction) error {
		data, _ := json.Marshal(d)
		for _, name := range []string{"intent", "state"} {
			if err := tx.Put(task, "dispatches/"+d.Input.ID+"/"+name+".json", string(data)+"\n"); err != nil {
				return err
			}
		}
		original, _ := json.Marshal(d.Input)
		return tx.PutGroup("00000000-dispatch-group", d.Input.ID+".json", string(original)+"\n")
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, task, d
}

func TestAcceptedNotifyUnprovenExitReturnsReceipt(t *testing.T) {
	for _, mode := range []string{"alive", "unknown", "pane-override"} {
		t.Run(mode, func(t *testing.T) {
			root, task, d := acceptedNotifyFixture(t)
			pane := ""
			if mode == "unknown" {
				t.Setenv("KANBAN_HERDR_GET_FAIL", "1")
			}
			if mode == "pane-override" {
				pane = "other-pane"
			}
			out, _, err := capture(t, func() error { return deliverDispatch(root, task, d.Input.ID, pane) })
			if err != nil || !strings.Contains(out, `"state":"accepted"`) {
				t.Fatalf("receipt lost: %s %v", out, err)
			}
			current, err := board.ReadDispatch(root, task, d.Input.ID)
			if err != nil || current.Authorization != d.Authorization || current.Execution != nil {
				t.Fatalf("unproven rotation: %+v %v", current, err)
			}
			for _, file := range []string{"herdr.log.run", "herdr.log.prompt"} {
				if _, err = os.Stat(filepath.Join(root, file)); !os.IsNotExist(err) {
					t.Fatal("unproven exit caused delivery", file)
				}
			}
		})
	}
}

func TestAcceptedNotifyRecoversExpiredIntentAndCompletes(t *testing.T) {
	root, task, d := acceptedNotifyFixture(t)
	// Only the old pane is gone; new-pane launch validation remains available.
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(s.Text, "herdr:w1:t9:w1:p9", "herdr:w1:t8:w1:p8", 1)
	if err = board.WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p8")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[]}}`)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "workspace-test")
	if err = os.WriteFile(filepath.Join(root, "fake-bin", "claude"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(root, "herdr.log.run")); err == nil {
				current, err := board.ReadDispatch(root, task, d.Input.ID)
				if err != nil {
					result <- err
					return
				}
				for _, state := range []string{"working", "review"} {
					s, err := board.ReadSnapshot(root, task)
					if err != nil {
						result <- err
						return
					}
					o := board.MoveOptions{Authorization: current.Authorization}
					if state == "review" {
						o.DeliveryCommit = strings.Repeat("b", 40)
					}
					if _, err = board.MoveWithOptions(s.Entry, root, state, o); err != nil {
						result <- err
						return
					}
				}
				result <- nil
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		result <- fmt.Errorf("recovery did not launch")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, _, err = capture(t, func() error { return deliverDispatchContext(ctx, root, task, d.Input.ID, "") })
	if e := <-result; e != nil {
		t.Fatal(e, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	current, err := board.ReadDispatch(root, task, d.Input.ID)
	if err != nil || current.State != board.DispatchCompleted || current.Authorization.Epoch != 2 || current.Execution == nil || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) || !current.AcceptBefore().After(time.Now()) {
		t.Fatalf("recovery facts: %+v %v", current, err)
	}
	run, err := os.ReadFile(filepath.Join(root, "herdr.log.run"))
	if err != nil || !strings.Contains(string(run), "claude") {
		t.Fatalf("missing recovery command: %s %v", run, err)
	}
	// A retry after completion reconciles without another epoch or launch.
	_, _, err = capture(t, func() error { return deliverDispatch(root, task, d.Input.ID, "") })
	after, readErr := os.ReadFile(filepath.Join(root, "herdr.log.run"))
	if err != nil || readErr != nil || string(after) != string(run) {
		t.Fatal("completion retry relaunched", err, readErr)
	}
}
