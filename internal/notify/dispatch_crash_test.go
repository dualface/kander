package notify

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestDispatchSendCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_NOTIFY_CRASH_ROOT")
	if root == "" {
		return
	}
	task := os.Getenv("KANDER_NOTIFY_CRASH_TASK")
	err := board.WithDispatchDelivery(root, "notify-one", func() error {
		d, err := board.ReadDispatch(root, task, "notify-one")
		if err != nil {
			return err
		}
		_, err = board.BeginDispatchAttempt(root, task, d.Input.ID, d.Revision)
		if err != nil {
			return err
		}
		if os.Getenv("KANDER_NOTIFY_CRASH_STAGE") == "after-send" {
			program, err := lookPath("herdr")
			if err != nil {
				return err
			}
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				return err
			}
			path, err := writeNotifyMessage(d.Input.Message)
			if err != nil {
				return err
			}
			if err = sendDispatch(context.Background(), DirectTarget{Kind: "herdr", Program: program, PaneID: "w1:p9"}, notifyInstruction(s.Entry, path, ackMarker())); err != nil {
				return err
			}
		}
		fmt.Println("send-crash-boundary")
		for {
			time.Sleep(time.Hour)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDispatchSendKillAndSameIDReconciliation(t *testing.T) {
	for _, stage := range []string{"before-send", "after-send"} {
		t.Run(stage, func(t *testing.T) {
			root, task, d := durableNotifyFixture(t, 10*time.Second)
			// This fake stays addressable after prompt delivery, like a real Agent TUI.
			fake := filepath.Join(root, "fake-bin", "herdr")
			script, err := os.ReadFile(fake)
			if err != nil {
				t.Fatal(err)
			}
			begin := strings.Index(string(script), `  if [ -f "$log.prompt" ]; then`)
			if begin < 0 {
				t.Fatal("fake condition missing")
			}
			end := begin + strings.Index(string(script)[begin:], "  fi\n") + len("  fi\n")
			if err = os.WriteFile(fake, append(script[:begin], script[end:]...), 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestDispatchSendCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_NOTIFY_CRASH_ROOT="+root, "KANDER_NOTIFY_CRASH_TASK="+task, "KANDER_NOTIFY_CRASH_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				ready <- scanner.Scan() && scanner.Text() == "send-crash-boundary"
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child failed")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("child timeout")
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err = cmd.Wait(); err == nil {
				t.Fatal("child not killed")
			}
			pending, err := board.ReadDispatch(root, task, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if pending.State != board.DispatchUnknown || pending.Accepted != nil {
				t.Fatal("crash lost uncertainty")
			}
			// The receiver can acknowledge the first delivery even after its sender died.
			result := make(chan error, 1)
			go func() { result <- acceptDelivered(root, task, d, false) }()
			_, _, err = capture(t, func() error {
				return board.WithDispatchDelivery(root, d.Input.ID, func() error { return deliverDispatch(root, task, d.Input.ID, "") })
			})
			if err != nil {
				t.Fatal(err)
			}
			if e := <-result; e != nil {
				t.Fatal(e)
			}
			current, err := board.ReadDispatch(root, task, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.State != board.DispatchAccepted || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) || current.Authorization != d.Authorization {
				t.Fatal("retry invented new execution/deadline")
			}
		})
	}
}
