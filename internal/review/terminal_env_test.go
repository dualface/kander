//go:build !windows

package review

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	terminaltest.ClearEnv()
	clearExecutionEnv()
	os.Exit(m.Run())
}
