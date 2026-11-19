//go:build darwin

package probe

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// procZombie is SZOMB in <sys/proc.h>; x/sys does not export it.
const procZombie = 5

// osGroupOnlyZombies reports whether process group pgid has no member that can
// still run: the group is empty, or every member is a zombie.
func osGroupOnlyZombies(pgid int) (bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return false, err
	}
	return onlyZombies(procs, pgid)
}

func onlyZombies(procs []unix.KinfoProc, pgid int) (bool, error) {
	only := true
	for _, p := range procs {
		if int(p.Eproc.Pgid) != pgid {
			return false, fmt.Errorf("process %d is in group %d, not in the requested group %d", p.Proc.P_pid, p.Eproc.Pgid, pgid)
		}
		if p.Proc.P_stat != procZombie {
			only = false
		}
	}
	return only, nil
}
