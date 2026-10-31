//go:build unix

package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/dualface/kander/internal/process"
)

type launchedProcess struct {
	cmd      *exec.Cmd
	tracker  *reviewerProcessTree
	waitDone chan struct{}
	waitErr  error
}

func launchProcess(inv process.ProcessInvocation, cwd string, stdin, stdout, stderr *os.File) (*launchedProcess, error) {
	cmd := exec.Command(inv.Argv[0], inv.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = flattenEnv(inv.Env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lp := &launchedProcess{cmd: cmd, waitDone: make(chan struct{})}
	go func() {
		lp.waitErr = lp.cmd.Wait()
		close(lp.waitDone)
	}()
	return lp, nil
}

func waitLeader(lp *launchedProcess, d time.Duration) {
	if lp == nil || lp.waitDone == nil {
		return
	}
	select {
	case <-lp.waitDone:
	case <-time.After(d):
	}
}

func processGroupExists(lp *launchedProcess) bool {
	if lp.waitDone != nil {
		select {
		case <-lp.waitDone:
			return processGroupHasLiveMembers(lp)
		default:
		}
	}
	if processGroupHasLiveMembers(lp) {
		return true
	}
	err := syscall.Kill(-lp.cmd.Process.Pid, 0)
	if err == syscall.ESRCH {
		return false
	}
	// Observable group with no live members is typically a zombie leader.
	// Wait for the dedicated Wait goroutine before treating the group as alive.
	waitLeader(lp, 50*time.Millisecond)
	if processGroupHasLiveMembers(lp) {
		return true
	}
	err = syscall.Kill(-lp.cmd.Process.Pid, 0)
	if err == syscall.ESRCH {
		return false
	}
	return err == nil || err == syscall.EPERM
}

func processGroupHasLiveMembers(lp *launchedProcess) bool {
	pid := lp.cmd.Process.Pid
	ctx, cancel := context.WithTimeout(context.Background(), processTableBudget)
	defer cancel()
	table, err := readProcessTable(ctx)
	if err != nil {
		err := syscall.Kill(-pid, 0)
		return err == nil || errors.Is(err, syscall.EPERM)
	}
	for _, info := range table {
		if info.pgid == pid && !info.zombie {
			return true
		}
	}
	return false
}

// stopProcessTree collects the reviewer process group. recheck is true only for a leader that exited with 0;
// the evidence stays in the result on every failure path.
func stopProcessTree(lp *launchedProcess, recheck bool) (processCollection, error) {
	if lp == nil || lp.cmd == nil || lp.cmd.Process == nil {
		return processCollection{}, nil
	}
	if !processGroupExists(lp) {
		waitLeader(lp, 5*time.Second)
		return processCollection{}, nil
	}
	collection, empty := observeReviewerGroup(lp, recheck)
	cleanup := &collection.evidence.Cleanup
	if empty {
		waitLeader(lp, 5*time.Second)
		cleanup.Collected = true
		return collection, nil
	}
	switch err := syscall.Kill(-lp.cmd.Process.Pid, syscall.SIGKILL); {
	case err == nil:
		cleanup.Sigkill = sigkillSent
	case errors.Is(err, syscall.ESRCH):
	default:
		cleanup.Sigkill = sigkillFailed
		cleanup.Error = err.Error()
		return collection, newGate(2,
			"review.could_not_terminate_the_posix_reviewer_process_group", err.Error(),
		)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processGroupHasLiveMembers(lp) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	gate := newGate(2, "review.could_not_collect_the_posix_reviewer_process_group")
	if processGroupHasLiveMembers(lp) {
		cleanup.Error = gate.message
		return collection, gate
	}
	select {
	case <-lp.waitDone:
	case <-time.After(5 * time.Second):
		cleanup.Error = gate.message
		return collection, gate
	}
	cleanup.Collected = true
	return collection, nil
}
