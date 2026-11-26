//go:build !unix

package menu

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	terminaltest.Main()
	terminaltest.ClearEnv()
	leave, err := enterNeutralDir()
	if err != nil {
		os.Exit(1)
	}
	code := m.Run()
	leave()
	os.Exit(code)
}
