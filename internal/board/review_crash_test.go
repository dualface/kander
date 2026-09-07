package board

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func reviewCrashBoundary() {
	fmt.Println("review-boundary")
	for {
		time.Sleep(time.Hour)
	}
}
func TestReviewCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_REVIEW_CRASH_ROOT")
	if root == "" {
		return
	}
	stage := os.Getenv("KANDER_REVIEW_CRASH_STAGE")
	ids := strings.Split(os.Getenv("KANDER_REVIEW_CRASH_TASKS"), ",")
	unlock, err := LockReviewRun(root, "crash")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	run, _, err := PrepareReviewRun(root, archiveInput(ids, "crash", "PM"), archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if stage == "intent" {
		reviewCrashBoundary()
	}
	if err = StoreReviewArtifact(root, run.RunID, "prompt.txt", []byte("original prompt")); err != nil {
		t.Fatal(err)
	}
	if stage == "prompt" {
		reviewCrashBoundary()
	}
	run.Phase = "launching"
	run.LaunchStatus = "unknown"
	if err = UpdateReviewRun(root, run); err != nil {
		t.Fatal(err)
	}
	if stage == "launching" {
		reviewCrashBoundary()
	}
	run.Phase = "running"
	run.LaunchStatus = "started"
	if err = UpdateReviewRun(root, run); err != nil {
		t.Fatal(err)
	}
	staging, err := ReviewStaging(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err = fs.WriteTextAtomic(staging, filepath.Join(staging, "output.raw"), "partial raw output", true); err != nil {
		t.Fatal(err)
	}
	if stage == "output" {
		reviewCrashBoundary()
	}
	run.ExecutionStatus = "ok"
	run, err = FinalizeReviewRun(root, run, []byte("final report"))
	if err != nil {
		t.Fatal(err)
	}
	if stage == "finalized" {
		reviewCrashBoundary()
	}
	_, failures, err := publishReviewRun(root, run.RunID, func(id string) {
		if stage == "first-card" && id == ids[0] {
			reviewCrashBoundary()
		}
	})
	if err != nil || len(failures) > 0 {
		t.Fatalf("%v %v", err, failures)
	}
	reviewCrashBoundary()
}

func TestReviewKillRestartBoundaries(t *testing.T) {
	for _, stage := range []string{"intent", "prompt", "launching", "output", "finalized", "first-card", "published"} {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{archiveCard(t, root, "crash-a"), archiveCard(t, root, "crash-b")}
			cmd := exec.Command(os.Args[0], "-test.run=^TestReviewCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_REVIEW_CRASH_ROOT="+root, "KANDER_REVIEW_CRASH_STAGE="+stage, "KANDER_REVIEW_CRASH_TASKS="+strings.Join(ids, ","))
			out, err := cmd.StdoutPipe()
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
				scanner := bufio.NewScanner(out)
				ready <- scanner.Scan() && scanner.Text() == "review-boundary"
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child exited early")
				}
			case <-time.After(15 * time.Second):
				t.Fatal("checkpoint timeout")
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if cmd.Wait() == nil {
				t.Fatal("child not killed")
			}
			// Recovery can reacquire the OS-owned run lease after the abrupt exit.
			unlock, err := LockReviewRun(root, "crash")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			run, err := ReadReviewRun(root, "crash")
			if err != nil {
				t.Fatal(err)
			}
			finalized := run.Phase == "finalized"
			if !finalized {
				run.ExecutionStatus = "interrupted"
				run.ExitCode = 2
				run.FailureReason = "gate interrupted; completion unconfirmed"
				run, err = FinalizeReviewRun(root, run, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			publishRun(t, root, run.RunID)
			publishRun(t, root, run.RunID)
			if err = ReviewPublicationComplete(root, run.RunID); err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				s := transactionSnapshot(t, root, id)
				indexes, err := ParseReviewIndexes(s.Text)
				if err != nil || len(indexes) != 1 {
					t.Fatalf("%v %v", indexes, err)
				}
				_, err = os.Stat(filepath.Join(s.Entry.Path, "reviews", "crash", "report.md"))
				if !finalized && !os.IsNotExist(err) {
					t.Fatal("interrupted run acquired a final report")
				}
			}
			problems, err := CheckReviewEvidence(root, ids)
			if err != nil || len(problems) > 0 {
				t.Fatalf("%v %v", problems, err)
			}
		})
	}
}
