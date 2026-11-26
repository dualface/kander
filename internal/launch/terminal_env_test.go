package launch

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	serveLineAgent()
	serveFakeGit()
	terminaltest.Main()
	terminaltest.ClearEnv()
	os.Exit(m.Run())
}
