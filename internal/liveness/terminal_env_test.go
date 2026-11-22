package liveness

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	terminaltest.ClearEnv()
	os.Exit(m.Run())
}
