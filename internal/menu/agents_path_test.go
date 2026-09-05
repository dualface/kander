package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestProjectCommandPathPrefersNativeBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kander.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isWindowsOS() {
		if got := projectCommandPath(dir, "kander"); got != exe {
			t.Fatalf("got %q want %q", got, exe)
		}
		return
	}
	if got := projectCommandPath(dir, "kander"); got != "" {
		t.Fatalf("posix should ignore kander.exe-only, got %q", got)
	}
	bin := filepath.Join(dir, "kander")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectCommandPath(dir, "kander"); got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestReviewGatePresentProjectMissingDoesNotUsePATH(t *testing.T) {
	dir := t.TempDir()
	paths := config.InstallPaths{Mode: config.ModeProject, BinDir: dir}
	ok, msg := reviewGatePresent(paths)
	if ok {
		t.Fatal("expected missing gate")
	}
	if !strings.Contains(msg, "审核入口不存在") && !strings.Contains(msg, "review entrypoint is missing") {
		t.Fatalf("%s", msg)
	}
	if strings.Contains(msg, "不在 PATH") || strings.Contains(msg, "not in PATH") || strings.Contains(msg, "~/.local/bin") {
		t.Fatalf("leaked global path: %s", msg)
	}
}

func TestCommandMissingMessageProjectRoot(t *testing.T) {
	dir := t.TempDir()
	paths := config.InstallPaths{Mode: config.ModeProject, BinDir: dir}
	msg := commandMissingMessage("kander", paths)
	if !strings.Contains(msg, "项目命令根") && !strings.Contains(msg, "project command root") {
		t.Fatalf("%s", msg)
	}
	if strings.Contains(msg, "PATH") {
		t.Fatalf("%s", msg)
	}
}
