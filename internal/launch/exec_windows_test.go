//go:build windows

package launch

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplyConsoleAttr(t *testing.T) {
	cmd := exec.Command("cmd")
	applyConsoleAttr(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	want := uint32(windows.CREATE_NEW_CONSOLE | windows.CREATE_NEW_PROCESS_GROUP)
	if cmd.SysProcAttr.CreationFlags&want != want {
		t.Fatalf("CreationFlags=%#x want bits %#x", cmd.SysProcAttr.CreationFlags, want)
	}
}
