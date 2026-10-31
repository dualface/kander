//go:build unix

package review

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type procInfo struct {
	ppid       int
	pgid       int
	zombie     bool
	startTime  string
	executable string
}

// processTableBudget bounds a snapshot taken outside a recheck.
const processTableBudget = 10 * time.Second

// readProcessTable is a variable so tests can inject process snapshots.
var readProcessTable = snapshotProcessTable

// snapshotProcessTable reads every process. An empty table is an error: this process is always in a real one.
func snapshotProcessTable(ctx context.Context) (map[int]procInfo, error) {
	table, err := readPlatformProcessTable(ctx)
	if err == nil && len(table) == 0 {
		err = errors.New("empty process table")
	}
	return table, err
}

func readPlatformProcessTable(ctx context.Context) (map[int]procInfo, error) {
	if runtime.GOOS == "linux" {
		return readProcFS()
	}
	return readPsTable(ctx)
}

func readProcFS() (map[int]procInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	table := map[int]procInfo{}
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
		if info, ok := parseProcStat(string(data)); ok {
			table[pid] = info
		}
	}
	return table, nil
}

// parseProcStat reads one /proc/<pid>/stat record. The command name is the kernel's comm, which holds no path
// or argv; it may contain spaces and parentheses, so the fixed fields are found from the last ")". Field 22 of
// proc(5) is the start time in clock ticks since boot.
func parseProcStat(record string) (procInfo, bool) {
	commandStart := strings.Index(record, "(")
	commandEnd := strings.LastIndex(record, ")")
	if commandStart < 0 || commandEnd < commandStart || commandEnd+2 > len(record) {
		return procInfo{}, false
	}
	fields := strings.Fields(record[commandEnd+2:])
	if len(fields) < 3 {
		return procInfo{}, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	pgid, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil {
		return procInfo{}, false
	}
	info := procInfo{ppid: ppid, pgid: pgid, zombie: fields[0] == "Z", executable: path.Base(record[commandStart+1 : commandEnd])}
	if len(fields) > 19 {
		info.startTime = fields[19]
	}
	return info, true
}

// readPsTable runs ps with separate -o options, because a shared one is not portable when a header is set
// empty. LC_ALL=C fixes the lstart layout that parsePsLine counts on.
func readPsTable(ctx context.Context) (map[int]procInfo, error) {
	cmd := exec.CommandContext(ctx, "ps", "-A", "-ww", "-o", "pid=", "-o", "ppid=", "-o", "pgid=", "-o", "state=", "-o", "lstart=", "-o", "comm=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	table := map[int]procInfo{}
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		if pid, info, ok := parsePsLine(scanner.Text()); ok {
			table[pid] = info
		}
	}
	return table, scanner.Err()
}

// psIdentityFields counts pid, ppid, pgid, state, and the five words of lstart. Everything after them is comm,
// which is a full path that may contain spaces on macOS. A shorter line, such as a zombie without a start
// time, keeps its process fields only.
const psIdentityFields = 9

func parsePsLine(line string) (int, procInfo, bool) {
	rest := line
	head := make([]string, 0, psIdentityFields)
	for len(head) < psIdentityFields {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			break
		}
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		head = append(head, rest[:end])
		rest = rest[end:]
	}
	if len(head) < 4 {
		return 0, procInfo{}, false
	}
	pid, err0 := strconv.Atoi(head[0])
	ppid, err1 := strconv.Atoi(head[1])
	pgid, err2 := strconv.Atoi(head[2])
	if err0 != nil || err1 != nil || err2 != nil {
		return 0, procInfo{}, false
	}
	info := procInfo{ppid: ppid, pgid: pgid, zombie: strings.HasPrefix(head[3], "Z")}
	if comm := strings.TrimSpace(rest); len(head) == psIdentityFields && comm != "" {
		info.startTime = strings.Join(head[4:], " ")
		info.executable = path.Base(comm)
	}
	return pid, info, true
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
	ctx, cancel := context.WithTimeout(context.Background(), processTableBudget)
	defer cancel()
	table, err := readProcessTable(ctx)
	if err != nil {
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
