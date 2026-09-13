package terminal_test

import (
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/dualface/kander/"

// deps lists the transitive imports of a package with the Go toolchain.
func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", module+pkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

func TestFoundationPackagesDoNotImportTerminal(t *testing.T) {
	for _, pkg := range []string{"internal/board", "internal/config", "internal/fs", "internal/process", "internal/i18n"} {
		for _, dep := range deps(t, pkg) {
			if dep == module+"internal/terminal" || strings.HasPrefix(dep, module+"internal/terminal/") {
				t.Errorf("%s imports %s", pkg, dep)
			}
		}
	}
}

func TestTerminalDoesNotImportCallers(t *testing.T) {
	callers := []string{"launch", "liveness", "notify", "takeover", "focus", "menu", "tui"}
	for _, pkg := range []string{"internal/terminal", "internal/terminal/builtin", "internal/terminal/direct", "internal/terminal/herdr", "internal/terminal/tmux"} {
		for _, dep := range deps(t, pkg) {
			for _, caller := range callers {
				if dep == module+"internal/"+caller {
					t.Errorf("%s imports %s", pkg, dep)
				}
			}
		}
	}
}
