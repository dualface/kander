//go:build !windows

package liveness

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestSubscriptionSignalsJoinBlockedOutputAndProbe(t *testing.T) {
	if os.Getenv("KANDER_RUNTIME_CHILD") == "1" {
		os.Exit(RunSubscribe([]string{"--refresh", ".02", "--heartbeat", "30", os.Getenv("KANDER_RUNTIME_GROUP"), os.Getenv("KANDER_RUNTIME_ID")}))
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			root, opts, id := factsMember(t)
			moveRuntimeWorking(t, root, id)
			setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
			dir := installBatchFake(t, false)
			_, writer := fullSubscriptionPipe(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSubscriptionSignalsJoinBlockedOutputAndProbe$")
			cmd.Env = append(os.Environ(), "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0", "KANDER_RUNTIME_CHILD=1", "KANDER_RUNTIME_GROUP="+opts.Group, "KANDER_RUNTIME_ID="+id)
			cmd.Stdout = writer
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			deadline := time.Now().Add(time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "w1:p0")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					cancel()
					<-done
					t.Fatalf("probe did not start: %s", stderr.String())
				}
				time.Sleep(time.Millisecond)
			}
			started := time.Now()
			if err := cmd.Process.Signal(sig); err != nil {
				cancel()
				<-done
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("signal exit: %v %s", err, stderr.String())
				}
			case <-time.After(500 * time.Millisecond):
				cancel()
				<-done
				t.Fatal("signal failed to join")
			}
			t.Logf("signal %s joined blocked writer and probe in %s", sig, time.Since(started))
			assertBatchPeakAndCleanup(t, dir, 1, 1)
		})
	}
}

func TestSubscriptionDoesNotOverlapProbeBatches(t *testing.T) {
	root, opts, id := factsMember(t)
	moveRuntimeWorking(t, root, id)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	for n := 1; n < 8; n++ {
		task, path := makeWorking(t, "runtime-batch-"+strconv.Itoa(n), "Worker")
		setTaskGroup(t, path, opts.Group)
		setLocation(t, path, "codex wanted", "herdr:w1:t1:w1:p"+strconv.Itoa(n))
		opts.Members = append(opts.Members, task)
	}
	dir := installBatchFake(t, false)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	// Several heartbeat deadlines pass while the first four workers stay slow.
	heartbeats := 0
	nextRuntimeEvent(t, events, func(e groupEvent) bool {
		if e.Event == "heartbeat" {
			heartbeats++
		}
		return heartbeats == 4
	})
	cancel()
	<-done
	assertBatchPeakAndCleanup(t, dir, DefaultBatchConcurrency, DefaultBatchConcurrency)
}
