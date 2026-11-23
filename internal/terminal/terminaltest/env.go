package terminaltest

import "os"

// terminalEnv names the variables that identify the caller's terminal or
// locate its service.
var terminalEnv = []string{
	"HC_TERMINAL_ID", "HC_SOCKET", "HC_SESSION",
	"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SOCKET_PATH",
	"LUVUS_ENV", "LUVUS_PANE_ID",
	"TMUX", "TMUX_PANE",
}

// ClearEnv removes the inherited terminal environment, so no test creates or
// drives a terminal of the developer who runs it. Every test package that
// depends on internal/terminal calls it in TestMain, after any fake program
// entry: a test process started as a fake keeps the values the test gave it.
func ClearEnv() {
	for _, name := range terminalEnv {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
}
