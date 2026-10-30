package process

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessIdentitySupported reports whether this platform can observe a process.
const ProcessIdentitySupported = true

// ObserveProcess reads kernel process metadata only: never the environment or the
// command line of the process. /proc/<pid>/exe names the executable, because a
// process can rewrite comm, and comm truncates at 15 bytes.
func ObserveProcess(ctx context.Context, pid int) (ProcessFacts, error) {
	if pid <= 0 {
		return ProcessFacts{}, fmt.Errorf("invalid process id")
	}
	if err := ctx.Err(); err != nil {
		return ProcessFacts{}, err
	}
	root := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := readProcessStat(root)
	if err != nil {
		return ProcessFacts{}, procError(err)
	}
	if stat.state == "Z" || stat.state == "X" {
		return ProcessFacts{}, ErrProcessGone
	}
	target, err := os.Readlink(filepath.Join(root, "exe"))
	if err != nil {
		return ProcessFacts{}, procError(err)
	}
	// An agent that updates itself replaces its binary while it runs.
	target = strings.TrimSuffix(target, " (deleted)")
	return ProcessFacts{PID: pid, ParentPID: stat.parent, Start: stat.start, Executable: filepath.Base(target)}, ctx.Err()
}

func procError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrProcessGone
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %v", ErrProcessDenied, err)
	}
	return err
}

// BootIdentity returns the kernel's identifier of this boot.
func BootIdentity() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", procError(err)
	}
	return strings.TrimSpace(string(data)), nil
}
