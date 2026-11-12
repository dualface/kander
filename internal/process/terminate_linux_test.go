package process

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestTerminateObservedPinsAndChecksIncarnation(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	facts, err := ObserveProcess(t.Context(), child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := BootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrong := facts
	wrong.Start += "-other"
	if err := TerminateObserved(t.Context(), wrong, boot); !errors.Is(err, ErrProcessReplaced) {
		t.Fatalf("unrelated incarnation accepted: %v", err)
	}
	if _, err := ObserveProcess(t.Context(), facts.PID); err != nil {
		t.Fatal("wrong incarnation was signaled", err)
	}
	if err := TerminateObserved(t.Context(), facts, "another-boot"); !errors.Is(err, ErrProcessReplaced) {
		t.Fatal(err)
	}
	if err := TerminateObserved(t.Context(), facts, boot); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recorded process did not stop")
	}
}
