package check

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPackageDoesNotImportTUI(t *testing.T) {
	data, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "github.com/dualface/kander/internal/check").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "github.com/dualface/kander/internal/tui" {
			t.Fatal("internal/check must not import internal/tui")
		}
	}
}
