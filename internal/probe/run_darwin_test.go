//go:build darwin

package probe

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// zombieGroup starts a group led by a blocking process and puts an exited,
// unreaped child (a zombie; this process is its parent) into it. The leader is
// returned still running.
func zombieGroup(t *testing.T) *exec.Cmd {
	t.Helper()
	leader := exec.Command("sleep", "30")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := leader.Process.Pid
	child := exec.Command("sleep", "0")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
	if err := child.Start(); err != nil {
		_ = leader.Process.Kill()
		_ = leader.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = leader.Wait()
		_ = child.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range procs {
			if int(p.Proc.P_pid) == child.Process.Pid && p.Proc.P_stat == procZombie {
				return leader
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d never became a zombie", child.Process.Pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestKillOfAGroupWithOnlyAZombieSucceeds(t *testing.T) {
	leader := zombieGroup(t)
	if err := syscall.Kill(leader.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := leader.Wait(); err == nil {
		t.Fatal("the killed leader reported success")
	}
	tree := &processTree{cmd: leader}
	if err := tree.kill(); err != nil {
		t.Fatalf("kill() of a group with only a zombie = %v, want nil", err)
	}
}

func TestOnlyZombiesSeesALiveMember(t *testing.T) {
	leader := zombieGroup(t)
	only, err := osGroupOnlyZombies(leader.Process.Pid)
	if err != nil || only {
		t.Fatalf("osGroupOnlyZombies with a live leader = (%v, %v), want (false, nil)", only, err)
	}
}

func TestOnlyZombiesClassifiesMembers(t *testing.T) {
	proc := func(pid int32, stat int8, pgid int32) unix.KinfoProc {
		var p unix.KinfoProc
		p.Proc.P_pid, p.Proc.P_stat, p.Eproc.Pgid = pid, stat, pgid
		return p
	}
	for _, tc := range []struct {
		name    string
		procs   []unix.KinfoProc
		want    bool
		wantErr bool
	}{
		{"empty", nil, true, false},
		{"zombies", []unix.KinfoProc{proc(1, procZombie, 7), proc(2, procZombie, 7)}, true, false},
		{"one live", []unix.KinfoProc{proc(1, procZombie, 7), proc(2, 2, 7)}, false, false},
		{"foreign group", []unix.KinfoProc{proc(1, procZombie, 8)}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := onlyZombies(tc.procs, 7)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("onlyZombies = (%v, %v), want (%v, error %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
