package ghcli

import (
	"os"
	"testing"

	"github.com/dualface/kander/internal/issue/ghcli/ghclitest"
)

// TestMain turns the test binary into the fake `gh`/`git` program when a parent
// test re-executes it through PATH.
func TestMain(m *testing.M) {
	ghclitest.Main()
	os.Exit(m.Run())
}
