//go:build windows

package launch

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type structuredProcessTree struct {
	cmd      *exec.Cmd
	job      windows.Handle
	mu       sync.Mutex
	closed   bool
	closeErr error
}

func newStructuredProcessTree(cmd *exec.Cmd) (*structuredProcessTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(job))
	}
	// The child cannot create descendants before the job owns it.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return &structuredProcessTree{cmd: cmd, job: job}, nil
}

func (tree *structuredProcessTree) start() (startErr error) {
	if err := tree.cmd.Start(); err != nil {
		return err
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(tree.cmd.Process.Pid),
	)
	if err != nil {
		return err
	}
	err = windows.AssignProcessToJobObject(tree.job, process)
	closeErr := windows.CloseHandle(process)
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}

	// os/exec closes the primary thread handle, so resume the still-suspended
	// thread found in one bounded snapshot, matching the probe runner strategy.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { startErr = errors.Join(startErr, windows.CloseHandle(snapshot)) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(tree.cmd.Process.Pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		return errors.Join(err, windows.CloseHandle(thread))
	}
	return errors.Join(errors.New("could not locate the suspended structured process thread"), err)
}

func (tree *structuredProcessTree) signal(bool) error {
	if tree == nil || tree.job == 0 {
		return nil
	}
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.closed {
		return nil
	}
	err := windows.TerminateJobObject(tree.job, 1)
	if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return err
}

func (tree *structuredProcessTree) close() error {
	if tree == nil || tree.job == 0 {
		return nil
	}
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.closed {
		return tree.closeErr
	}
	tree.closed = true
	tree.closeErr = windows.CloseHandle(tree.job)
	return tree.closeErr
}

func watchStructuredSignals(*structuredProcessTree, <-chan struct{}) func() { return func() {} }
