package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ObserveOpenFiles reads only kernel process metadata. PID reuse and descriptor
// changes are checked again by callers before they use the returned evidence.
func ObserveOpenFiles(ctx context.Context, pid int) (ProcessFiles, error) {
	if pid <= 0 {
		return ProcessFiles{}, fmt.Errorf("invalid process id")
	}
	if err := ctx.Err(); err != nil {
		return ProcessFiles{}, err
	}
	root := filepath.Join("/proc", strconv.Itoa(pid))
	identity, err := processIncarnation(root)
	if err != nil {
		return ProcessFiles{}, err
	}
	directory, err := os.Open(filepath.Join(root, "fd"))
	if err != nil {
		return ProcessFiles{}, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(4097)
	if err != nil && err != io.EOF {
		return ProcessFiles{}, err
	}
	if len(entries) > 4096 {
		return ProcessFiles{}, fmt.Errorf("process open-file observation exceeds 4096 descriptors")
	}
	result := ProcessFiles{Identity: strconv.Itoa(pid) + ":" + identity}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return ProcessFiles{}, err
		}
		fd := filepath.Join(root, "fd", entry.Name())
		path, err := os.Readlink(fd)
		if err != nil || !filepath.IsAbs(path) || strings.HasSuffix(path, " (deleted)") {
			continue
		}
		info, err := os.Stat(fd)
		if err == nil && info.Mode().IsRegular() {
			result.Files = append(result.Files, OpenFile{Path: path, Info: info})
		}
	}
	after, err := processIncarnation(root)
	if err != nil {
		return ProcessFiles{}, err
	}
	if after != identity {
		return ProcessFiles{}, fmt.Errorf("process changed during open-file observation")
	}
	return result, ctx.Err()
}

func processIncarnation(root string) (string, error) {
	file, err := os.Open(filepath.Join(root, "stat"))
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if len(data) > 16384 || end < 0 {
		return "", fmt.Errorf("invalid process incarnation")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("missing process start time")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", fmt.Errorf("invalid process start time")
	}
	return fields[19], nil
}
