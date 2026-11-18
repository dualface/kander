package process

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processStat holds the fields of /proc/<pid>/stat that identify a process.
type processStat struct {
	state  string
	parent int
	start  string
}

// readProcessStat parses after the last ')', because the name field can hold any
// byte.
func readProcessStat(root string) (processStat, error) {
	file, err := os.Open(filepath.Join(root, "stat"))
	if err != nil {
		return processStat{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return processStat{}, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if len(data) > 16384 || end < 0 {
		return processStat{}, fmt.Errorf("invalid process incarnation")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return processStat{}, fmt.Errorf("missing process start time")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return processStat{}, fmt.Errorf("invalid process start time")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return processStat{}, fmt.Errorf("invalid parent process id")
	}
	return processStat{state: fields[0], parent: parent, start: fields[19]}, nil
}
