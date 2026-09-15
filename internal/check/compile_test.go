package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsCrossCompile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("already running on windows")
	}
	out := filepath.Join(t.TempDir(), "check.test.exe")
	cmd := exec.Command("go", "test", "-c", "-o", out, ".")
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("windows cross-compile: %v\n%s", err, data)
	}
}

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
