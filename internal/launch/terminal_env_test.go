package launch

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	serveLineAgent()
	terminaltest.Main()
	terminaltest.ClearEnv()
	os.Exit(m.Run())
}
