//go:build windows

package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteBinaryOccupiedExecutableRenameAside(t *testing.T) {
	home := setupInstallHome(t)
	dest := filepath.Join(home, ".local", "bin", binaryName())
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", dest, filepath.Join("testdata", "hold.go"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("go build helper: %v\n%s", err, out)
	}
	old, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	proc := exec.Command(dest)
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = proc.Process.Kill()
		_ = proc.Wait()
	}()
	time.Sleep(200 * time.Millisecond)
	if err := writeBinary(dest, []byte("new-bytes")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new-bytes" {
		t.Fatalf("dest=%q err=%v", got, err)
	}
	aside, err := os.ReadFile(dest + ".old")
	if err != nil {
		t.Fatalf("expected rename-aside file: %v", err)
	}
	if string(aside) != string(old) {
		t.Fatalf("aside did not keep the occupied executable")
	}
}
