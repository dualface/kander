package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const identityHelperEnv = "KANDER_PROCESS_IDENTITY_HELPER"

func startScript(t *testing.T, name, body string) *exec.Cmd {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// A shim execs the real binary after it started: the settled reading names the
// binary that keeps running, and the configured agent name appears nowhere.
func TestObserveSettledFollowsAShimExec(t *testing.T) {
	cmd := startScript(t, "claude", "#!/bin/sh\nsleep 0.2\nexec sleep 30\n")
	facts, err := ObserveSettled(context.Background(), cmd.Process.Pid, alwaysRunning)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Executable != "sleep" {
		t.Fatalf("facts=%+v", facts)
	}
	later, err := ObserveProcess(context.Background(), cmd.Process.Pid)
	if err != nil || later != facts {
		t.Fatalf("later %+v err=%v, recorded %+v", later, err, facts)
	}
}

// A shim that works longer than the quiet window looks settled while it still runs
// the interpreter. Only the start file proves an image, so the reading stays
// unconfirmed, and the evidence of a running agent gives the final one.
func TestQuietDoesNotConfirmALateShim(t *testing.T) {
	cmd := startScript(t, "claude", "#!/bin/sh\nsleep 1.2\nexec sleep 30\n")
	early, err := ObserveSettled(context.Background(), cmd.Process.Pid, alwaysRunning)
	if err != nil {
		t.Fatal(err)
	}
	if early.Executable == "sleep" || ImageConfirmed(cmd.Path, early) {
		t.Fatalf("early=%+v", early)
	}
	began := time.Now()
	ready := func() bool { return time.Since(began) > 1500*time.Millisecond }
	final, err := ObserveWhenReady(context.Background(), early, alwaysRunning, ready)
	if err != nil {
		t.Fatal(err)
	}
	if final.Executable != "sleep" || final.Start != early.Start {
		t.Fatalf("final=%+v early=%+v", final, early)
	}
}

func TestObserveSettledReportsAnExitDuringTheWait(t *testing.T) {
	cmd := startScript(t, "short", "#!/bin/sh\nexec sleep 0.2\n")
	if _, err := ObserveSettled(context.Background(), cmd.Process.Pid, alwaysRunning); !errors.Is(err, ErrProcessGone) {
		t.Fatalf("err=%v", err)
	}
}

// Until a parent reaps it, an exited child stays in /proc as a zombie.
func TestObserveProcessTreatsAZombieAsGone(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	root := filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid))
	deadline := time.Now().Add(10 * time.Second)
	for {
		stat, err := readProcessStat(root)
		if err != nil {
			t.Fatal(err)
		}
		if stat.state == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no zombie: %+v", stat)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := ObserveProcess(context.Background(), cmd.Process.Pid); !errors.Is(err, ErrProcessGone) {
		t.Fatalf("err=%v", err)
	}
}

// TestIdentityHelperSleep is the renamed agent of the deleted-binary test.
func TestIdentityHelperSleep(t *testing.T) {
	if os.Getenv(identityHelperEnv) != "" {
		time.Sleep(time.Minute)
	}
}

// An agent that updates itself deletes the binary it runs from. The test copies
// its own binary: a system sleep can be a multi-call binary that refuses a new name.
func TestObserveProcessIgnoresADeletedBinary(t *testing.T) {
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent-1.2.3")
	if err = os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "-test.run=^TestIdentityHelperSleep$")
	cmd.Env = append(os.Environ(), identityHelperEnv+"=1")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	facts, err := ObserveProcess(context.Background(), cmd.Process.Pid)
	if err != nil || facts.Executable != "agent-1.2.3" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

// The process name field may hold spaces and parentheses.
func TestReadProcessStatParsesAfterTheLastParenthesis(t *testing.T) {
	root := t.TempDir()
	stat := "42 (a b) c) S 7 42 42 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 12345 0 0\n"
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readProcessStat(root)
	if err != nil || got != (processStat{state: "S", parent: 7, start: "12345"}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestProcErrorSeparatesDenialFromAbsence(t *testing.T) {
	if !errors.Is(procError(os.ErrNotExist), ErrProcessGone) || !errors.Is(procError(os.ErrPermission), ErrProcessDenied) {
		t.Fatal("wrong classification")
	}
	other := errors.New("io")
	if !errors.Is(procError(other), other) {
		t.Fatal("other error lost")
	}
}
