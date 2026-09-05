//go:build windows

package review

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/dualface/kander/internal/process"
	"golang.org/x/sys/windows"
)

type launchedProcess struct {
	cmd         *exec.Cmd
	job         windows.Handle
	eventHandle windows.Handle
	waitDone    chan struct{}
	waitErr     error
}

func launchProcess(inv process.ProcessInvocation, cwd string, stdin, stdout, stderr *os.File) (*launchedProcess, error) {
	job, err := createWindowsJob()
	if err != nil {
		return nil, err
	}
	eventName := windowsEventName()
	event, err := windows.CreateEvent(nil, 1, 0, windows.StringToUTF16Ptr(eventName))
	if err != nil {
		closeHandle(job)
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		closeHandle(event)
		closeHandle(job)
		return nil, err
	}
	argv := append([]string{self, "review", windowsJobBootstrap, eventName}, inv.Argv...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = flattenEnv(inv.Env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		closeHandle(event)
		closeHandle(job)
		return nil, err
	}
	lp := &launchedProcess{cmd: cmd, job: job, eventHandle: event, waitDone: make(chan struct{})}
	go func() {
		lp.waitErr = lp.cmd.Wait()
		close(lp.waitDone)
	}()
	proc, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		select {
		case <-lp.waitDone:
		case <-time.After(5 * time.Second):
		}
		closeHandle(event)
		closeHandle(job)
		return nil, err
	}
	assignErr := windows.AssignProcessToJobObject(job, proc)
	_ = windows.CloseHandle(proc)
	if assignErr != nil {
		_ = cmd.Process.Kill()
		select {
		case <-lp.waitDone:
		case <-time.After(5 * time.Second):
		}
		closeHandle(event)
		closeHandle(job)
		return nil, assignErr
	}
	if err := windows.SetEvent(event); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		closeHandle(event)
		closeHandle(job)
		return nil, err
	}
	return lp, nil
}

func createWindowsJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		closeHandle(job)
		return 0, err
	}
	return job, nil
}

func windowsEventName() string {
	return "KanderReview-" + itoa(os.Getpid()) + "-" + randomHex(16)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return itoa(os.Getpid())
	}
	return hex.EncodeToString(b)
}

func closeHandle(h windows.Handle) {
	if h != 0 {
		_ = windows.CloseHandle(h)
	}
}

type jobBasicAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func jobActiveProcesses(job windows.Handle) (int, error) {
	var info jobBasicAccounting
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	if err != nil {
		return 0, err
	}
	return int(info.ActiveProcesses), nil
}

func stopProcessTree(lp *launchedProcess) (bool, error) {
	if lp == nil {
		return false, nil
	}
	defer func() {
		closeHandle(lp.eventHandle)
		lp.eventHandle = 0
	}()
	if lp.job == 0 {
		return false, newGate(2, "review.reviewer_process_is_not_protected_by_a_windows_job")
	}
	defer func() {
		closeHandle(lp.job)
		lp.job = 0
	}()
	active, err := jobActiveProcesses(lp.job)
	if err != nil {
		return false, newGate(2, "review.could_not_collect_the_windows_reviewer_process_tree", err.Error())
	}
	lingering := active > 0
	if active > 0 {
		if err := windows.TerminateJobObject(lp.job, 1); err != nil {
			return false, newGate(2, "review.could_not_collect_the_windows_reviewer_process_tree", err.Error())
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for active > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		active, err = jobActiveProcesses(lp.job)
		if err != nil {
			return false, newGate(2, "review.could_not_collect_the_windows_reviewer_process_tree", err.Error())
		}
	}
	if active > 0 {
		return false, newGate(2, "review.could_not_collect_the_windows_reviewer_process_tree_windows")
	}
	select {
	case <-lp.waitDone:
	case <-time.After(5 * time.Second):
		return false, newGate(2, "review.windows_reviewer_process_did_not_exit_after_its_job")
	}
	return lingering, nil
}
