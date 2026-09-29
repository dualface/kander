package process

import (
	"runtime"
	"strings"
)

// AgentPath renders a filesystem path for text an execution agent reads: prompts, task files, and
// notification lines. On Windows it uses forward slashes, because agents often paste these paths
// into Git Bash, which treats a backslash as an escape; forward slashes also work in PowerShell and
// in the agents' file tools. POSIX paths are returned unchanged. Paths used to open files, build
// argv, write card metadata, or compare stay native; only the displayed text changes.
func AgentPath(path string) string {
	return agentPath(path, runtime.GOOS == "windows")
}

func agentPath(path string, windows bool) string {
	if !windows {
		return path
	}
	return strings.ReplaceAll(path, `\`, "/")
}
