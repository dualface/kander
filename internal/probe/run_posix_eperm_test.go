//go:build !windows

package probe

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestKillClassifiesEPERMOfAGroupWithoutLiveMembers(t *testing.T) {
	errCheck := errors.New("cannot read members")
	for _, tc := range []struct {
		name       string
		killErr    error
		only       bool
		checkErr   error
		want       error
		wantChecks int
	}{
		{"zombies only", syscall.EPERM, true, nil, nil, 1},
		{"live member", syscall.EPERM, false, nil, syscall.EPERM, 1},
		{"check failed", syscall.EPERM, false, errCheck, syscall.EPERM, 1},
		{"check failed with only", syscall.EPERM, true, errCheck, syscall.EPERM, 1},
		{"no such group", syscall.ESRCH, false, nil, nil, 0},
		{"other error", syscall.EACCES, true, nil, syscall.EACCES, 0},
		{"signal sent", nil, true, nil, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var killed, checks int
			oldKill, oldCheck := killGroup, groupOnlyZombies
			t.Cleanup(func() { killGroup, groupOnlyZombies = oldKill, oldCheck })
			killGroup = func(pid int, sig syscall.Signal) error {
				if pid != -4242 || sig != syscall.SIGKILL {
					t.Fatalf("kill(%d, %v), want kill(-4242, SIGKILL)", pid, sig)
				}
				killed++
				return tc.killErr
			}
			groupOnlyZombies = func(pgid int) (bool, error) {
				if pgid != 4242 {
					t.Fatalf("checked group %d, want 4242", pgid)
				}
				checks++
				return tc.only, tc.checkErr
			}
			tree := &processTree{cmd: &exec.Cmd{Process: &os.Process{Pid: 4242}}}
			// The error is returned as is, never wrapped.
			if got := tree.kill(); got != tc.want {
				t.Fatalf("kill() = %v, want %v", got, tc.want)
			}
			if killed != 1 || checks != tc.wantChecks {
				t.Fatalf("killed %d times and checked %d times, want 1 and %d", killed, checks, tc.wantChecks)
			}
		})
	}
}
