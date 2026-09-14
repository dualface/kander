package install

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInstallDoesNotImportTUI(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{join .Deps \"\\n\"}}", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(dep, "/internal/tui") {
			t.Fatalf("internal/install depends on internal/tui: %s", dep)
		}
	}
}
