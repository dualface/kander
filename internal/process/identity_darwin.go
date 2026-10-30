package process

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/sys/unix"
)

// ProcessIdentitySupported reports whether this platform can observe a process.
const ProcessIdentitySupported = true

// ImageCanChange reports whether a running process can replace its image.
const ImageCanChange = true

// szomb is SZOMB of <sys/proc.h>; x/sys does not export it.
const szomb = 5

// ObserveProcess reads the kernel process table only: never the environment or
// the command line of the process.
func ObserveProcess(ctx context.Context, pid int) (ProcessFacts, error) {
	if pid <= 0 {
		return ProcessFacts{}, fmt.Errorf("invalid process id")
	}
	if err := ctx.Err(); err != nil {
		return ProcessFacts{}, err
	}
	// The slice form reports a missing process as an empty result; the single
	// form cannot tell it from a short read.
	found, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return ProcessFacts{}, sysctlError(err)
	}
	if len(found) == 0 || found[0].Proc.P_stat == szomb {
		return ProcessFacts{}, ErrProcessGone
	}
	proc := found[0]
	start := proc.Proc.P_starttime
	return ProcessFacts{
		PID:        pid,
		ParentPID:  int(proc.Eproc.Ppid),
		Start:      strconv.FormatInt(int64(start.Sec), 10) + "." + strconv.FormatInt(int64(start.Usec), 10),
		Executable: unix.ByteSliceToString(proc.Proc.P_comm[:]),
	}, ctx.Err()
}

func sysctlError(err error) error {
	switch {
	case errors.Is(err, unix.ESRCH), errors.Is(err, unix.ENOENT):
		return ErrProcessGone
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return fmt.Errorf("%w: %v", ErrProcessDenied, err)
	}
	return err
}

// BootIdentity returns the boot time in Unix seconds.
func BootIdentity() (string, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", sysctlError(err)
	}
	return strconv.FormatInt(int64(boot.Sec), 10), nil
}
