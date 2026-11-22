package main

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	serveFakeHC()
	terminaltest.ClearEnv()
	os.Exit(m.Run())
}
