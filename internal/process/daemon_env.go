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

// EnvStartOrder names the start order a kander start runs for. The resident
// worker sets it for the start it runs; kander start removes it from its own
// environment before it starts anything, so no agent, terminal server, or
// later command inherits it.
const EnvStartOrder = "KANDER_START_ORDER"

// EnvManaged marks a process a service manager started from a service definition that kander
// service install wrote: a server or a worker in managed mode. A detached child never inherits
// it; the only exception is the successor of a managed worker on Windows, which its parent
// passes it to explicitly.
const EnvManaged = "KANDER_MANAGED"

// Managed reports whether this process runs in managed mode.
func Managed() bool { return os.Getenv(EnvManaged) == "1" }

// DaemonEnvironment is the environment of a detached long-lived child: the
// current environment without the execution variables, EnvStartOrder, and EnvManaged, followed by extra
// ("NAME=value" entries) so an extra entry wins over an inherited one.
func DaemonEnvironment(extra ...string) []string {
	return daemonEnvironment(os.Environ(), runtime.GOOS == "windows", extra)
}

func daemonEnvironment(env []string, foldCase bool, extra []string) []string {
	out := make([]string, 0, len(env)+len(extra))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if sameEnvName(name, EnvExecutionID, foldCase) || sameEnvName(name, EnvCoordinatorExecutionID, foldCase) || sameEnvName(name, EnvStartOrder, foldCase) || sameEnvName(name, EnvManaged, foldCase) {
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
