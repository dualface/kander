package launch

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	serveLineAgent()
	serveFakeGit()
	terminaltest.Main()
	terminaltest.ClearEnv()
	clearExecutionEnv()
	os.Exit(m.Run())
}

// clearExecutionEnv drops the execution identity an executing agent session
// carries. remote.Session picks only that execution's credential when the
// variable is set, so the tests would depend on the runner's session; a test
// that needs a value sets it with t.Setenv. It runs after the fake program
// entry points so a re-entered child still sees the values a test passed.
func clearExecutionEnv() {
	for _, name := range []string{process.EnvExecutionID, process.EnvCoordinatorExecutionID} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
}
