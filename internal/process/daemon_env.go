package process

import (
	"os"
	"runtime"
	"strings"
)

// The variables that bind a process to one agent execution. They belong to
// that execution only, never to a long-lived process another command starts.
const (
	EnvExecutionID            = "KANDER_EXECUTION_ID"
	EnvCoordinatorExecutionID = "KANDER_COORDINATOR_EXECUTION_ID"
)

// DaemonEnvironment is the environment of a detached long-lived child: the
// current environment without the execution variables, followed by extra
// ("NAME=value" entries) so an extra entry wins over an inherited one.
func DaemonEnvironment(extra ...string) []string {
	return daemonEnvironment(os.Environ(), runtime.GOOS == "windows", extra)
}

func daemonEnvironment(env []string, foldCase bool, extra []string) []string {
	out := make([]string, 0, len(env)+len(extra))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if sameEnvName(name, EnvExecutionID, foldCase) || sameEnvName(name, EnvCoordinatorExecutionID, foldCase) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, extra...)
}

// sameEnvName compares names the way the platform does: Windows ignores case.
func sameEnvName(name, want string, foldCase bool) bool {
	if foldCase {
		return strings.EqualFold(name, want)
	}
	return name == want
}
