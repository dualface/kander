//go:build unix

package review

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type procInfo struct {
	ppid   int
	pgid   int
	zombie bool
}

func snapshotProcessTable() map[int]procInfo {
	table := map[int]procInfo{}
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return map[int]procInfo{}
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
			if err != nil {
				continue
			}
			record := string(data)
			commandEnd := strings.LastIndex(record, ")")
			if commandEnd < 0 {
				continue
			}
			fields := strings.Fields(record[commandEnd+2:])
			if len(fields) < 3 {
				continue
			}
			ppid, err1 := strconv.Atoi(fields[1])
			pgid, err2 := strconv.Atoi(fields[2])
			if err1 != nil || err2 != nil {
				continue
			}
			table[pid] = procInfo{ppid: ppid, pgid: pgid, zombie: fields[0] == "Z"}
		}
		return table
	}
	cmd := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,state=")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return map[int]procInfo{}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return map[int]procInfo{}
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return map[int]procInfo{}
	}
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		pid, err0 := strconv.Atoi(fields[0])
		ppid, err1 := strconv.Atoi(fields[1])
		pgid, err2 := strconv.Atoi(fields[2])
		if err0 != nil || err1 != nil || err2 != nil {
			continue
		}
		table[pid] = procInfo{ppid: ppid, pgid: pgid, zombie: strings.HasPrefix(fields[3], "Z")}
	}
	return table
}

type reviewerProcessTree struct {
	leaderPID int
	ownTree   map[int]struct{}
	observed  bool
}

func newReviewerProcessTree(leaderPID int) *reviewerProcessTree {
	return &reviewerProcessTree{
		leaderPID: leaderPID,
		ownTree:   map[int]struct{}{leaderPID: {}},
	}
}

func (t *reviewerProcessTree) observe() {
	table := snapshotProcessTable()
	if len(table) == 0 {
		return
	}
	t.observed = true
	pending := map[int]struct{}{}
	for pid, info := range table {
		if info.pgid == t.leaderPID && !info.zombie {
			if _, ok := t.ownTree[pid]; !ok {
				pending[pid] = struct{}{}
			}
		}
	}
	progressed := true
	for progressed {
		progressed = false
		for pid := range pending {
			if _, ok := t.ownTree[table[pid].ppid]; ok {
				t.ownTree[pid] = struct{}{}
				delete(pending, pid)
				progressed = true
			}
		}
	}
}

func (t *reviewerProcessTree) unattributedMembers() (members []int, ok bool) {
	table := snapshotProcessTable()
	if len(table) == 0 {
		return nil, false
	}
	for pid, info := range table {
		if info.pgid == t.leaderPID && !info.zombie {
			if _, own := t.ownTree[pid]; !own {
				members = append(members, pid)
			}
		}
	}
	return members, true
}
