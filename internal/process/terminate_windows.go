package process

import (
	"context"
	"golang.org/x/sys/windows"
	"strconv"
)

// TerminateObserved verifies the start token on the same handle that receives
// termination, so a later process reusing the PID cannot be terminated.
func TerminateObserved(ctx context.Context, facts ProcessFacts, boot string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentBoot, err := BootIdentity()
	if err != nil {
		return err
	}
	if boot == "" || facts.Start == "" || !SameBoot(boot, currentBoot) {
		return ErrProcessReplaced
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(facts.PID))
	if err != nil {
		return processError(err)
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return processError(err)
	}
	start := strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10)
	if start != facts.Start {
		return ErrProcessReplaced
	}
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return processError(err)
	}
	if code != stillActive {
		return nil
	}
	return windows.TerminateProcess(handle, 1)
}
