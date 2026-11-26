package focus

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	terminaltest.ClearEnv()
	os.Exit(m.Run())
}
