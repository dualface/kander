//go:build windows

package probe

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processTree struct {
	cmd *exec.Cmd
	job windows.Handle
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(job))
	}
	// Assign the suspended child before any of its code can spawn descendants.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return &processTree{cmd: cmd, job: job}, nil
}

func (tree *processTree) start(ctx context.Context) (startErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tree.cmd.Start(); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(tree.cmd.Process.Pid))
	if err != nil {
		return err
	}
	err = windows.AssignProcessToJobObject(tree.job, process)
	closeErr := windows.CloseHandle(process)
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// os/exec closes the primary thread handle. Locate that still-suspended thread
	// in one snapshot; never retry enumeration beyond the caller's budget.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { startErr = errors.Join(startErr, windows.CloseHandle(snapshot)) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if err := ctx.Err(); err != nil {
			return err
		}
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
	return errors.Join(probeError("probe.process_setup_failed"), err)
}

func (tree *processTree) kill() error  { return windows.TerminateJobObject(tree.job, 1) }
func (tree *processTree) close() error { return windows.CloseHandle(tree.job) }
