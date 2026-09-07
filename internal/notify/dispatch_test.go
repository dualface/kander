package notify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

func durableNotifyFixture(t *testing.T, duration time.Duration) (string, string, board.Dispatch) {
	t.Helper()
	root, _ := setupBoard(t)
	task, path := makeReview(t, root, "durable")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	created := time.Now().UTC()
	d, err := board.PrepareDispatch(root, board.DispatchInput{ID: "notify-one", TaskID: task, Kind: "sync", Message: "修复问题", Base: strings.Repeat("a", 40), CreatedAt: created, ConfirmBy: created.Add(duration)})
	if err != nil {
		t.Fatal(err)
	}
	return root, task, d
}

func TestPromptEchoDoesNotCountAsAcknowledgement(t *testing.T) {
	root, task, d := durableNotifyFixture(t, 400*time.Millisecond)
	out, _, err := capture(t, func() error { return deliverDispatch(root, task, d.Input.ID, "") })
	if err == nil || !strings.Contains(out, `"state":"delivery-unknown"`) {
		t.Fatalf("prompt echo accepted: %s %v", out, err)
	}
	current, err := board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if current.Accepted != nil || current.Attempts != 1 || s.Entry.State != "review" {
		t.Fatalf("echo fabricated receipt: %+v", current)
	}
	if _, err = os.Stat(filepath.Join(root, "herdr.log.prompt")); err != nil {
		t.Fatal("prompt not sent")
	}
	if _, err = os.Stat(filepath.Join(root, "herdr.log.wait")); !os.IsNotExist(err) {
		t.Fatal("terminal echo was used as business acknowledgement")
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	if !strings.Contains(string(prompt), "KANDER-NOTIFY-ACK:") {
		t.Fatal("transport diagnostic marker missing")
	}
	_, _, err = capture(t, func() error {
		return commandNotify(root, task, "different payload", "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
	})
	if err == nil {
		t.Fatal("different retry message accepted")
	}
	_, _, err = capture(t, func() error {
		return commandNotify(root, task, d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
	})
	if err == nil {
		t.Fatal("expired retry reported accepted")
	}
	current, _ = board.ReadDispatch(root, task, d.Input.ID)
	if current.Attempts != 1 || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatal("retry reset deadline or sent another round")
	}
}

func acceptDelivered(root, task string, d board.Dispatch, finish bool) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "herdr.log.prompt")); err == nil {
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				return err
			}
			_, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization})
			if err != nil {
				return err
			}
			if finish {
				s, err = board.ReadSnapshot(root, task)
				if err != nil {
					return err
				}
				_, err = board.MoveWithOptions(s.Entry, root, "review", board.MoveOptions{Authorization: d.Authorization, DeliveryCommit: strings.Repeat("b", 40)})
				if err != nil {
					return err
				}
			}
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("no prompt")
}

func TestDispatchReceiptBeforeNotifyReturnAndRetry(t *testing.T) {
	root, task, d := durableNotifyFixture(t, 5*time.Second)
	result := make(chan error, 1)
	go func() { result <- acceptDelivered(root, task, d, true) }()
	out, _, err := capture(t, func() error {
		return commandNotify(root, task+".md", d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, `"accepted":`) {
		t.Fatalf("missing receipt: %s", out)
	}
	current, err := board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != board.DispatchCompleted {
		t.Fatal(current)
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.order"))
	out, _, err = capture(t, func() error {
		return commandNotify(root, task+".md", d.Input.Message, "", "", true, 61, launch.DispatchOptions{ID: d.Input.ID})
	})
	if err != nil || !strings.Contains(out, `"state":"completed"`) {
		t.Fatalf("retry: %s %v", out, err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "herdr.log.order"))
	if string(after) != string(prompt) {
		t.Fatal("accepted retry sent again")
	}
}

func TestDispatchUnknownProbeAndBusyDoNotRecover(t *testing.T) {
	for _, mode := range []string{"unknown", "busy"} {
		t.Run(mode, func(t *testing.T) {
			root, task, d := durableNotifyFixture(t, 400*time.Millisecond)
			if mode == "unknown" {
				t.Setenv("KANBAN_HERDR_GET_FAIL", "1")
			} else {
				t.Setenv("KANBAN_HERDR_STATUS", "working")
			}
			_, _, err := capture(t, func() error { return deliverDispatch(root, task, d.Input.ID, "") })
			if err == nil {
				t.Fatal("unproven delivery succeeded")
			}
			current, e := board.ReadDispatch(root, task, d.Input.ID)
			if e != nil {
				t.Fatal(e)
			}
			if current.State != board.DispatchPrepared || current.Attempts != 0 {
				t.Fatal("probe created send attempt")
			}
			for _, file := range []string{"herdr.log.prompt", "herdr.log.run", "herdr.log.order"} {
				if _, err = os.Stat(filepath.Join(root, file)); !os.IsNotExist(err) {
					t.Fatalf("probe caused transport/launch: %s", file)
				}
			}
		})
	}
}

func TestDispatchFailedSendKeepsConsumedNewBody(t *testing.T) {
	root, task, d := durableNotifyFixture(t, 5*time.Second)
	if _, err := board.BeginDispatchAttempt(root, task, d.Input.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
	stale, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(stale.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	current, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, task, board.UpdateOptions{Document: "spec.md", Text: current.Text + "\n执行端新记录\n", ExpectedRevision: current.Revision, Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	if err = board.RollbackDocument(root, stale.Entry, stale.Text, "review"); err == nil {
		t.Fatal("failed send erased acceptance")
	}
	_, _, err = capture(t, func() error {
		return reconcileDispatch(root, task, d.Input.ID, fmt.Errorf("send failed after delivery"))
	})
	if err != nil {
		t.Fatal(err)
	}
	current, _ = board.ReadSnapshot(root, task)
	if current.Entry.State != "working" || !strings.Contains(current.Text, "执行端新记录") {
		t.Fatal("new body lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = capture(t, func() error { return awaitDispatch(ctx, root, task, d.Input.ID) })
	if err != nil {
		t.Fatal("persisted acceptance lost at timeout", err)
	}
}

func TestDurableExplicitPaneOverridesUnknownRecordedWindow(t *testing.T) {
	root, task, d := durableNotifyFixture(t, 5*time.Second)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(s.Text, "herdr:w1:t9:w1:p9", "foreground", 1)
	if err = board.WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- acceptDelivered(root, task, d, false) }()
	_, _, err = capture(t, func() error { return deliverDispatch(root, task, d.Input.ID, "w1:p9") })
	if err != nil {
		t.Fatal(err)
	}
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if board.MetadataFrom(s.Text, "WINDOW") != "herdr:w1:t9:w1:p9" {
		t.Fatal("override address not persisted")
	}
}

func TestDispatchRetryHonorsShorterInvocationDeadline(t *testing.T) {
	root, task, d := durableNotifyFixture(t, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err := capture(t, func() error { return deliverDispatchContext(ctx, root, task, d.Input.ID, "") })
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("invocation deadline ignored: %v", err)
	}
	current, err := board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) || current.State != board.DispatchUnknown {
		t.Fatal("short retry rewrote durable deadline")
	}
}
