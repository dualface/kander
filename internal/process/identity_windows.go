package process

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessIdentitySupported reports whether this platform can observe a process.
const ProcessIdentitySupported = true

// ImageCanChange reports whether a running process can replace its image. Windows
// has no exec.
const ImageCanChange = false

// stillActive is STILL_ACTIVE of the Win32 process API.
const stillActive = 259

// ObserveProcess reads kernel process metadata only: never the environment or the
// command line of the process. A handle keeps an exited process readable, so the
// exit code decides whether it still runs.
func ObserveProcess(ctx context.Context, pid int) (ProcessFacts, error) {
	if pid <= 0 {
		return ProcessFacts{}, fmt.Errorf("invalid process id")
	}
	if err := ctx.Err(); err != nil {
		return ProcessFacts{}, err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ProcessFacts{}, processError(err)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err = windows.GetExitCodeProcess(handle, &code); err != nil {
		return ProcessFacts{}, processError(err)
	}
	if code != stillActive {
		return ProcessFacts{}, ErrProcessGone
	}
	var created, exited, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return ProcessFacts{}, processError(err)
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return ProcessFacts{}, processError(err)
	}
	parent, err := parentProcess(uint32(pid))
	if err != nil {
		return ProcessFacts{}, err
	}
	start := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	return ProcessFacts{
		PID:        pid,
		ParentPID:  parent,
		Start:      strconv.FormatUint(start, 10),
		Executable: filepath.Base(windows.UTF16ToString(buffer[:size])),
	}, ctx.Err()
}

// parentProcess walks one snapshot. Windows keeps the parent ID after the parent
// exits, so a caller must verify the parent's own identity.
func parentProcess(pid uint32) (int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, processError(err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == pid {
			return int(entry.ParentProcessID), nil
		}
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, ErrProcessGone
	}
	return 0, err
}

func processError(err error) error {
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return ErrProcessGone
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return fmt.Errorf("%w: %v", ErrProcessDenied, err)
	}
	return err
}

// BootIdentity derives the boot time in Unix seconds. Windows has no boot
// identifier, so SameBoot compares this value with a tolerance.
func BootIdentity() (string, error) {
	boot := time.Now().Add(-windows.DurationSinceBoot())
	return strconv.FormatInt(boot.Unix(), 10), nil
}
